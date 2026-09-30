package immobilienscout24

// The attachments of the fake API in fake_api_test.go. They mirror what the
// live sandbox did on 2026-09-30:
//
//   - POST .../realestate/{id}/attachment takes a picture or a PDF document
//     as multipart/form-data, with a part named metadata and a part named
//     attachment in either order, and a link as a plain XML body. It answers
//     201 with a Location header and a messages body that carries the new id;
//   - the metadata is a common:attachment with xsi:type common:Picture,
//     common:PDFDocument or common:Link. Its elements must be in schema order,
//     a picture needs floorplan and titlePicture, a PDF document floorplan;
//     otherwise the answer is 412 ERROR_COMMON_SCHEMA_VALIDATION_FAILED with
//     the text of the schema validator. externalCheckSum is stored as sent;
//   - GET .../attachment lists the attachments of the real estate, and
//     ?externalId= those with that external id, as an empty list when there
//     is none; GET .../attachment/{id} answers one. Both carry the xlink:href,
//     id and modification attributes, the urls of a picture and the url of a
//     PDF document;
//   - PUT .../attachment/{id} replaces the metadata: what it leaves out, the
//     checksum included, is cleared;
//   - a real estate with pictures has exactly one title picture. The first
//     picture uploaded is it, also with titlePicture false; a PUT with
//     titlePicture true moves it to that picture, and false is ignored.
//     Deleting the title picture makes the next picture the title picture;
//   - GET .../attachment/attachmentsorder lists the pictures and PDF
//     documents, not the links. A picture that becomes the title picture
//     moves to the first place, so the title picture is the first picture;
//   - DELETE .../attachment/{id} answers 200, and 404 ERROR_RESOURCE_NOT_FOUND
//     when repeated. Deleting the real estate deletes its attachments.
//
// Simulated, not observed: that an upload with titlePicture true takes the
// title picture, which the developer FAQ implies ("Consequently is always the
// latest uploaded image on top"); the checks of the part headers, which follow
// the Insert page; the texts of the 404, PUT and DELETE responses; how every
// other invalid request fails; the length limits of IS24's per-file XSD for
// title, externalId and a link's url; and the path of a PDF document's url.

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeAttachmentFirstID is the id the sandbox gave the probe's first picture.
const fakeAttachmentFirstID = 904864036

// observedAttachmentNamespaces are the namespace declarations on every
// attachment response of the sandbox.
const observedAttachmentNamespaces = `xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" ` +
	`xmlns:offerlistelement="http://rest.immobilienscout24.de/schema/offer/listelement/1.0" ` +
	`xmlns:ns5="http://rest.immobilienscout24.de/schema/search/expose/1.0" ` +
	`xmlns:ns6="http://rest.immobilienscout24.de/schema/customer/1.0" ` +
	`xmlns:realestates="http://rest.immobilienscout24.de/schema/offer/realestates/1.0" ` +
	`xmlns:ns8="http://rest.immobilienscout24.de/schema/attachmentsorder/1.0" ` +
	`xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" ` +
	`xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" ` +
	`xmlns:videoupload="http://rest.immobilienscout24.de/schema/videoupload/1.0" ` +
	`xmlns:ns12="http://rest.immobilienscout24.de/schema/entitlement/1.0"`

type fakeAttachment struct {
	realEstateID string
	xsiType      string            // the local part, such as attachmentPicture
	fields       map[string]string // the metadata elements but titlePicture
	modification string
	// contentSHA256 and contentType describe the uploaded file.
	contentSHA256, contentType string
}

// fakeAttachmentState is the attachment side of fakeAPI, guarded by fakeAPI.mu.
type fakeAttachmentState struct {
	// sequences holds the element sequence of each type; it never changes.
	sequences   map[string][]xsdElement
	nextID      int
	attachments map[string]*fakeAttachment
	// order holds the pictures and PDF documents of each real estate in the
	// attachment order. The first picture in it is the title picture.
	order map[string][]string
	// created and removedOutOfBand record attachment history for CheckDestroy.
	created          []string
	removedOutOfBand map[string]bool
	// stealTitle, when set, becomes the title picture right after the next
	// attachment write, as a concurrent write by another client would.
	stealTitle string
}

func newFakeAttachmentState(t testing.TB) fakeAttachmentState {
	s := fakeAttachmentState{
		sequences:        map[string][]xsdElement{},
		nextID:           fakeAttachmentFirstID,
		attachments:      map[string]*fakeAttachment{},
		order:            map[string][]string{},
		removedOutOfBand: map[string]bool{},
	}
	for _, typ := range []string{attachmentPicture, attachmentPDF, attachmentLink} {
		s.sequences[typ] = attachmentSequence(mustElements(t, commonNamespace, typ))
	}
	return s
}

// attachmentSequence adds externalCheckSum after externalId to a sequence of
// the WADL-bundled XSD in testdata, which predates it. IS24's per-file
// common-1.0.xsd has it there, and so do the sandbox's schema errors.
func attachmentSequence(els []xsdElement) []xsdElement {
	var out []xsdElement
	for _, e := range els {
		out = append(out, e)
		if e.Name == "externalId" {
			out = append(out, xsdElement{Name: "externalCheckSum", Type: "xs:string", Optional: true})
		}
	}
	return out
}

// AttachmentIDs returns the ids of every attachment, sorted.
func (f *fakeAPI) AttachmentIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id := range f.att.attachments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// AttachmentField returns an element of an attachment, and whether it is set.
func (f *fakeAPI) AttachmentField(id, element string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.att.attachments[id]
	if !ok {
		return "", false
	}
	v, ok := a.fields[element]
	return v, ok
}

// AttachmentUpload returns the SHA-256 and the media type of the file of an attachment.
func (f *fakeAPI) AttachmentUpload(id string) (sha256Hex, contentType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.att.attachments[id]; ok {
		return a.contentSHA256, a.contentType
	}
	return "", ""
}

// AttachmentOrder returns the attachment order of a real estate and its title picture.
func (f *fakeAPI) AttachmentOrder(realEstateID string) (order []string, titlePicture string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.att.order[realEstateID]), f.titlePicture(realEstateID)
}

// AttachmentHistory returns the ids of the attachments created through the
// API, and the set of those removed out of band.
func (f *fakeAPI) AttachmentHistory() ([]string, map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	oob := map[string]bool{}
	for id := range f.att.removedOutOfBand {
		oob[id] = true
	}
	return slices.Clone(f.att.created), oob
}

// AddPictureOutOfBand adds a picture as other software would, with checksum
// as its externalCheckSum, or without one when checksum is empty, and returns
// its id.
func (f *fakeAPI) AddPictureOutOfBand(realEstateID, checksum string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := strconv.Itoa(f.att.nextID)
	f.att.nextID++
	a := &fakeAttachment{realEstateID: realEstateID, xsiType: attachmentPicture, modification: fakeModification(),
		fields: map[string]string{"title": "anonymized", "floorplan": "false"}, contentType: "image/jpeg"}
	if checksum != "" {
		a.fields["externalCheckSum"] = checksum
	}
	f.att.attachments[id] = a
	f.att.order[realEstateID] = append(f.att.order[realEstateID], id)
	return id
}

// StealTitlePictureAfterNextWrite makes the picture id the title picture
// right after the next attachment POST or PUT, as a concurrent write would.
func (f *fakeAPI) StealTitlePictureAfterNextWrite(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.att.stealTitle = id
}

// SetAttachmentOutOfBand changes an element of an attachment as another
// client's PUT would; an empty value removes the element.
func (f *fakeAPI) SetAttachmentOutOfBand(id, element, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if value == "" {
		delete(f.att.attachments[id].fields, element)
		return
	}
	f.att.attachments[id].fields[element] = value
}

// handleAttachment serves the attachments of a real estate, and reports false
// for any other path.
func (f *fakeAPI) handleAttachment(w http.ResponseWriter, r *http.Request, body []byte) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, fakeCollectionPath)
	realEstateID, sub, found := strings.Cut(rest, "/")
	if !ok || !found || (sub != "attachment" && !strings.HasPrefix(sub, "attachment/")) {
		return false
	}
	id := strings.TrimPrefix(strings.TrimPrefix(sub, "attachment"), "/")
	f.mu.Lock()
	_, exists := f.objects[realEstateID]
	f.mu.Unlock()
	switch {
	case !exists:
		// Observed for a GET after the real estate was deleted; the text is simulated.
		writeAttachmentMessage(w, http.StatusNotFound, codeResourceNotFound,
			"Resource [realestate] with id ["+realEstateID+"] not found.", "")
	case id == "" && r.Method == http.MethodPost:
		f.createAttachment(w, r.Header.Get("Content-Type"), realEstateID, body)
	case id == "" && r.Method == http.MethodGet:
		f.listAttachments(w, realEstateID, r.URL.Query())
	case id == "attachmentsorder" && r.Method == http.MethodGet:
		f.mu.Lock()
		order := f.renderAttachmentOrder(realEstateID)
		f.mu.Unlock()
		writeRaw(w, http.StatusOK, order)
	case isDigits(id):
		f.attachmentItem(w, r, realEstateID, id, body)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
	return true
}

func (f *fakeAPI) createAttachment(w http.ResponseWriter, contentType, realEstateID string, body []byte) {
	var a *fakeAttachment
	var makeTitle bool
	mt, params, err := mime.ParseMediaType(contentType)
	switch {
	case err == nil && mt == "multipart/form-data":
		a, makeTitle, err = f.parseUpload(body, params["boundary"])
	case err == nil && mt == mediaTypeXML:
		a, makeTitle, err = f.parseMetadata(body)
		if err == nil && a.xsiType != attachmentLink {
			err = fmt.Errorf("fake API: a common:%s is uploaded as multipart/form-data with its file", a.xsiType)
		}
		// Observed on the sandbox (2026-09-30): a link without a title gets "Link".
		if err == nil && a.fields["title"] == "" {
			a.fields["title"] = "Link"
		}
	default:
		err = errFakeMediaType
	}
	if err != nil {
		writeAttachmentError(w, err)
		return
	}

	f.mu.Lock()
	id := strconv.Itoa(f.att.nextID)
	f.att.nextID++
	a.realEstateID, a.modification = realEstateID, fakeModification()
	f.att.attachments[id] = a
	f.att.created = append(f.att.created, id)
	if a.xsiType != attachmentLink {
		f.att.order[realEstateID] = append(f.att.order[realEstateID], id)
	}
	if makeTitle {
		f.moveToFront(realEstateID, id)
	}
	f.wroteAttachment()
	f.mu.Unlock()

	w.Header().Set("Location", f.BaseURL()+"/offer/v1.0/user/me/realestate/"+realEstateID+"/attachment/"+id)
	writeAttachmentMessage(w, http.StatusCreated, codeResourceCreated, "Resource [attachment] with id ["+id+"] has been created.", id)
}

func (f *fakeAPI) listAttachments(w http.ResponseWriter, realEstateID string, query url.Values) {
	keep := func(*fakeAttachment) bool { return true }
	if query.Has("externalId") {
		externalID := query.Get("externalId")
		keep = func(a *fakeAttachment) bool { return a.fields["externalId"] == externalID }
	}
	f.mu.Lock()
	list := f.renderAttachmentList(realEstateID, keep)
	f.mu.Unlock()
	writeRaw(w, http.StatusOK, list)
}

func (f *fakeAPI) attachmentItem(w http.ResponseWriter, r *http.Request, realEstateID, id string, body []byte) {
	var update *fakeAttachment
	var makeTitle bool
	if r.Method == http.MethodPut {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && mt != mediaTypeXML {
			err = errFakeMediaType
		}
		if err == nil {
			update, makeTitle, err = f.parseMetadata(body)
		}
		if err != nil {
			writeAttachmentError(w, err)
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.att.attachments[id]
	if !ok || a.realEstateID != realEstateID {
		// The code is the sandbox's; the text is simulated.
		writeAttachmentMessage(w, http.StatusNotFound, codeResourceNotFound, "Resource [attachment] with id ["+id+"] not found.", "")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeRaw(w, http.StatusOK, f.renderAttachment(id))
	case http.MethodPut:
		if update.xsiType != a.xsiType {
			// Simulated: whether the type can change was not observed.
			writeMessages(w, http.StatusBadRequest, "ERROR_COMMON_BAD_REQUEST",
				"fake API: attachment "+id+" is a common:"+a.xsiType+", not a common:"+update.xsiType)
			return
		}
		a.fields, a.modification = update.fields, fakeModification()
		if makeTitle {
			f.moveToFront(realEstateID, id)
		}
		f.wroteAttachment()
		writeAttachmentMessage(w, http.StatusOK, "MESSAGE_RESOURCE_UPDATED", "Resource [attachment] with id ["+id+"] has been updated.", id)
	case http.MethodDelete:
		f.removeAttachment(id)
		writeAttachmentMessage(w, http.StatusOK, "MESSAGE_RESOURCE_DELETED", "Resource [attachment] with id ["+id+"] has been deleted.", id)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

// titlePicture returns the title picture of a real estate, the first picture
// in its attachment order. The caller holds f.mu.
func (f *fakeAPI) titlePicture(realEstateID string) string {
	for _, id := range f.att.order[realEstateID] {
		if f.att.attachments[id].xsiType == attachmentPicture {
			return id
		}
	}
	return ""
}

// wroteAttachment applies StealTitlePictureAfterNextWrite. The caller holds f.mu.
func (f *fakeAPI) wroteAttachment() {
	if id := f.att.stealTitle; id != "" {
		f.att.stealTitle = ""
		f.moveToFront(f.att.attachments[id].realEstateID, id)
	}
}

// moveToFront makes a picture the title picture. The caller holds f.mu.
func (f *fakeAPI) moveToFront(realEstateID, id string) {
	rest := slices.DeleteFunc(slices.Clone(f.att.order[realEstateID]), func(o string) bool { return o == id })
	f.att.order[realEstateID] = append([]string{id}, rest...)
}

// removeAttachment deletes an attachment. The caller holds f.mu.
func (f *fakeAPI) removeAttachment(id string) {
	a := f.att.attachments[id]
	delete(f.att.attachments, id)
	f.att.order[a.realEstateID] = slices.DeleteFunc(f.att.order[a.realEstateID], func(o string) bool { return o == id })
}

// removeAttachments deletes the attachments of a real estate, recording them
// as removed out of band when outOfBand is set. The caller holds f.mu.
func (f *fakeAPI) removeAttachments(realEstateID string, outOfBand bool) {
	for id, a := range f.att.attachments {
		if a.realEstateID == realEstateID {
			delete(f.att.attachments, id)
			if outOfBand {
				f.att.removedOutOfBand[id] = true
			}
		}
	}
	delete(f.att.order, realEstateID)
}

// fakeModification is the modification attribute of an attachment written
// now, in the sandbox's format.
func fakeModification() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
