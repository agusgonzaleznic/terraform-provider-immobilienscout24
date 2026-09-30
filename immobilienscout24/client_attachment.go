package immobilienscout24

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
)

// attachmentsPath is the attachment collection of a real estate.
func attachmentsPath(realEstateID string) string {
	return realEstatePath + url.PathEscape(realEstateID) + "/attachment"
}

func attachmentPath(realEstateID, id string) string {
	return attachmentsPath(realEstateID) + "/" + url.PathEscape(id)
}

// attachmentFile is the file part of an upload.
type attachmentFile struct {
	// Name is the file name the part carries, see uploadFileName.
	Name        string
	ContentType string
	Content     []byte
}

// UploadAttachment uploads a picture or PDF document with its metadata and
// returns the new attachment id. The API cannot change the file afterwards.
func (c *Client) UploadAttachment(ctx context.Context, realEstateID string, doc *attachmentDocument, file attachmentFile) (string, error) {
	metadata, err := marshalAttachment(doc)
	if err != nil {
		return "", err
	}
	body, contentType, err := multipartBody(metadata, file)
	if err != nil {
		return "", err
	}
	resp, err := c.send(ctx, c.uploadClient, http.MethodPost, attachmentsPath(realEstateID), contentType, body)
	if err != nil {
		return "", err
	}
	return createdAttachmentID(resp)
}

// CreateAttachment creates an attachment without a file, a link, from a plain
// XML body, and returns its id.
func (c *Client) CreateAttachment(ctx context.Context, realEstateID string, doc *attachmentDocument) (string, error) {
	body, err := marshalAttachment(doc)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, attachmentsPath(realEstateID), body)
	if err != nil {
		return "", err
	}
	return createdAttachmentID(resp)
}

func createdAttachmentID(resp *response) (string, error) {
	id, detail := createdID(resp, scoutID)
	if id == "" {
		return "", fmt.Errorf("the API accepted the attachment (HTTP %d) but the provider could not determine its id (%s). "+
			"The attachment probably exists on the listing now and has to be removed or imported by hand", resp.status, detail)
	}
	return id, nil
}

// GetAttachment retrieves an attachment of a real estate by id.
func (c *Client) GetAttachment(ctx context.Context, realEstateID, id string) (*attachmentDocument, error) {
	resp, err := c.do(ctx, http.MethodGet, attachmentPath(realEstateID, id), nil)
	if err != nil {
		return nil, err
	}
	return unmarshalAttachment(id, resp.body)
}

// UpdateAttachment replaces the metadata of an attachment. PUT is a full
// replacement (observed 2026-09-30): an element that doc leaves out is
// cleared, externalCheckSum included.
func (c *Client) UpdateAttachment(ctx context.Context, realEstateID, id string, doc *attachmentDocument) error {
	body, err := marshalAttachment(doc)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPut, attachmentPath(realEstateID, id), body)
	return err
}

// DeleteAttachment deletes an attachment. A repeated DELETE answers 404
// ERROR_RESOURCE_NOT_FOUND (observed 2026-09-30), which matches ErrNotFound.
func (c *Client) DeleteAttachment(ctx context.Context, realEstateID, id string) error {
	_, err := c.do(ctx, http.MethodDelete, attachmentPath(realEstateID, id), nil)
	return err
}

// claimTitlePicture records that a picture of the listing is about to be
// written with titlePicture true, and reports false when one already was
// through this client. Terraform writes every resource at most once per
// apply, so a second claim means that two pictures of the listing set
// title_picture = true. releaseTitlePicture undoes a claim whose write failed.
func (c *Client) claimTitlePicture(realEstateID string) bool {
	c.titleMu.Lock()
	defer c.titleMu.Unlock()
	if c.titlePictures[realEstateID] {
		return false
	}
	c.titlePictures[realEstateID] = true
	return true
}

func (c *Client) releaseTitlePicture(realEstateID string) {
	c.titleMu.Lock()
	defer c.titleMu.Unlock()
	delete(c.titlePictures, realEstateID)
}

// multipartBody builds the documented upload: a part named metadata with the
// XML as body.xml, then a part named attachment with the file ("it has to be
// written as" either name). The headers are those of the documented raw
// request, which the sandbox accepted, in either part order (observed
// 2026-09-30). The oauth1 transport leaves the body out of the signature, as
// RFC 5849 does for every body that is not form-encoded.
func multipartBody(metadata []byte, file attachmentFile) ([]byte, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, part := range []struct {
		name, fileName, contentType string
		content                     []byte
	}{
		{"metadata", "body.xml", mediaTypeXML, metadata},
		{"attachment", file.Name, file.ContentType, file.Content},
	} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, part.name, part.fileName))
		header.Set("Content-Type", part.contentType+"; name="+part.fileName)
		header.Set("Content-Transfer-Encoding", "binary")
		pw, err := w.CreatePart(header)
		if err != nil {
			return nil, "", fmt.Errorf("encoding the %s part: %w", part.name, err)
		}
		if _, err := pw.Write(part.content); err != nil {
			return nil, "", fmt.Errorf("encoding the %s part: %w", part.name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("encoding the upload: %w", err)
	}
	return b.Bytes(), w.FormDataContentType(), nil
}

// uploadFileName is the base name of a path with every character other than
// an ASCII letter, a digit, '.', '-' and '_' replaced by '_', so that it goes
// into the part headers as it is, without quoting or encoding.
func uploadFileName(path string) string {
	return strings.Map(func(r rune) rune {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') || strings.ContainsRune(".-_", r) {
			return r
		}
		return '_'
	}, filepath.Base(path))
}
