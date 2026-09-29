package immobilienscout24

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dghubble/oauth1"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Base URLs from https://api.immobilienscout24.de/api-docs/basic-principles/
// and https://api.immobilienscout24.de/api-docs/sandbox/.
const (
	sandboxBaseURL    = "https://rest.sandbox-immobilienscout24.de/restapi/api"
	productionBaseURL = "https://rest.immobilienscout24.de/restapi/api"

	// realEstatePath is the collection resource for the authenticated user.
	// "me" is the documented stand-in for the username under 3-legged OAuth.
	realEstatePath = "/offer/v1.0/user/me/realestate/"
	// publishPath is the publish resource. A publication, one real estate on
	// one publish channel, has the id "{realEstateID}_{channelID}".
	publishPath = "/offer/v1.0/publish"
	// contactPath is the contact resource of the authenticated user.
	contactPath = "/offer/v1.0/user/me/contact"

	mediaTypeXML = "application/xml"

	// maxResponseBytes caps how much of a response body is read at all.
	maxResponseBytes = 1 << 20
	// maxErrorBodyBytes caps how much of an unparseable body goes into an error.
	maxErrorBodyBytes = 1024
)

// Message codes from messages-1.0.xsd and the Responses page.
const (
	codeResourceCreated        = "MESSAGE_RESOURCE_CREATED"
	codeResourceNotFound       = "ERROR_RESOURCE_NOT_FOUND"
	codeCommonResourceNotFound = "ERROR_COMMON_RESOURCE_NOT_FOUND"
	codeRequestConflict        = "ERROR_COMMON_REQUEST_CONFLICT"
	codeResourceValidation     = "ERROR_RESOURCE_VALIDATION"
)

// ErrNotFound matches, via errors.Is, an APIError that the API documented as
// "resource not found". A bare 404 without such a message code does not match,
// because the API also answers 404 for an unsupported Accept header.
var ErrNotFound = errors.New("resource not found")

// ErrConflict matches, via errors.Is, a 409 ERROR_COMMON_REQUEST_CONFLICT
// response. The sandbox answers that way when a listing is published on a
// channel it is already published on (observed 2026-09-29).
var ErrConflict = errors.New("request conflict")

// ErrDefaultContact matches, via errors.Is, the 412 ERROR_RESOURCE_VALIDATION
// with which the sandbox refuses to delete the account's default contact
// (observed 2026-09-29).
var ErrDefaultContact = errors.New("the default contact cannot be deleted")

// defaultContactUndeletable matches the text of that refusal: "Error while
// validating input for the resource. [MESSAGE: default contact can not be
// deleted. Please provide assigntocontactid query parameter]".
var defaultContactUndeletable = regexp.MustCompile(`(?i)default contact can ?not be deleted`)

// Client talks to the ImmobilienScout24 Import/Export API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string

	// publishMu makes publish requests wait for each other; see Publish.
	publishMu sync.Mutex
}

// NewClient returns a client that signs every request with OAuth 1.0a
// (HMAC-SHA1). baseURL is the API root, e.g. sandboxBaseURL.
func NewClient(baseURL, consumerKey, consumerSecret, accessToken, accessTokenSecret, userAgent string) *Client {
	config := oauth1.NewConfig(consumerKey, consumerSecret)
	token := oauth1.NewToken(accessToken, accessTokenSecret)
	return &Client{
		httpClient: newHTTPClient(config, token),
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  userAgent,
	}
}

// requestTimeout bounds every API call, so a hung connection cannot stall a
// Terraform run indefinitely.
const requestTimeout = 60 * time.Second

// newHTTPClient signs requests and never follows redirects: the documented API
// does not redirect, and following one would send a freshly signed request to
// whatever host the redirect names.
func newHTTPClient(config *oauth1.Config, token *oauth1.Token) *http.Client {
	c := config.Client(context.Background(), token)
	c.Timeout = requestTimeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// Message is one entry of a <common:messages> response.
type Message struct {
	Code string `xml:"messageCode"`
	Text string `xml:"message"`
	ID   string `xml:"id"`
}

type messagesDocument struct {
	XMLName  xml.Name  `xml:"messages"`
	Messages []Message `xml:"message"`
}

// APIError is a non-2xx response. It never carries request headers, so it
// cannot leak the OAuth Authorization header.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Messages   []Message
	// Body holds the start of the response body when it was not a
	// <common:messages> document, truncated to maxErrorBodyBytes.
	Body string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: HTTP %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	for _, m := range e.Messages {
		fmt.Fprintf(&b, "\n%s: %s", m.Code, strings.TrimSpace(m.Text))
	}
	if len(e.Messages) == 0 && e.Body != "" {
		fmt.Fprintf(&b, "\n%s", e.Body)
	}
	return b.String()
}

// Codes returns the message codes of the error response.
func (e *APIError) Codes() []string {
	codes := make([]string, 0, len(e.Messages))
	for _, m := range e.Messages {
		codes = append(codes, m.Code)
	}
	return codes
}

// Is reports whether the error is a documented not-found or conflict response,
// or the refusal to delete the default contact.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound && e.hasCode(codeResourceNotFound, codeCommonResourceNotFound)
	case ErrConflict:
		return e.StatusCode == http.StatusConflict && e.hasCode(codeRequestConflict)
	case ErrDefaultContact:
		return e.StatusCode == http.StatusPreconditionFailed && slices.ContainsFunc(e.Messages, func(m Message) bool {
			return m.Code == codeResourceValidation && defaultContactUndeletable.MatchString(m.Text)
		})
	}
	return false
}

func (e *APIError) hasCode(codes ...string) bool {
	for _, m := range e.Messages {
		if slices.Contains(codes, m.Code) {
			return true
		}
	}
	return false
}

// CreateApartmentRent inserts a real estate and returns its scout id.
func (c *Client) CreateApartmentRent(ctx context.Context, doc *apartmentRentDocument) (string, error) {
	body, err := marshalApartmentRent(doc)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, realEstatePath, body)
	if err != nil {
		return "", err
	}
	id, detail := createdID(resp, scoutID)
	if id == "" {
		return "", fmt.Errorf("the API accepted the real estate (HTTP %d) but the provider could not determine its id (%s). "+
			"The object probably exists in the account now and has to be removed or imported by hand", resp.status, detail)
	}
	return id, nil
}

// GetApartmentRent retrieves a real estate by scout id.
func (c *Client) GetApartmentRent(ctx context.Context, id string) (*apartmentRentDocument, error) {
	resp, err := c.do(ctx, http.MethodGet, realEstatePath+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	return unmarshalApartmentRent(resp.body)
}

// UpdateApartmentRent replaces a real estate. The API treats PUT as a full
// replacement, so doc must hold every attribute, not only the changed ones.
func (c *Client) UpdateApartmentRent(ctx context.Context, id string, doc *apartmentRentDocument) error {
	body, err := marshalApartmentRent(doc)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPut, realEstatePath+url.PathEscape(id), body)
	return err
}

// DeleteRealEstate deletes a real estate of any type by scout id.
func (c *Client) DeleteRealEstate(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, realEstatePath+url.PathEscape(id), nil)
	return err
}

// Publish publishes a real estate on a publish channel and returns the id of
// the publication. The documentation asks to "send the POST publish requests
// one after the other and not in parallel", because each one checks the
// realtor's quota. Terraform creates resources in parallel, so publish
// requests through one Client wait for each other.
func (c *Client) Publish(ctx context.Context, realEstateID, channelID string) (string, error) {
	body, err := marshalPublishRequest(realEstateID, channelID)
	if err != nil {
		return "", err
	}
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	resp, err := c.do(ctx, http.MethodPost, publishPath, body)
	if err != nil {
		return "", err
	}
	want := publicationID(realEstateID, channelID)
	id, detail := createdID(resp, publicationIDPattern)
	switch {
	case id == "":
		return "", fmt.Errorf("the API accepted the publication (HTTP %d) but the provider could not determine its id (%s). "+
			"The listing is probably published now; import the publication with the id %s", resp.status, detail, want)
	case id != want:
		return "", fmt.Errorf("the API accepted the publication (HTTP %d) but reported the id %s instead of %s", resp.status, id, want)
	}
	return id, nil
}

// GetPublication retrieves a publication by its id.
func (c *Client) GetPublication(ctx context.Context, id string) (*Publication, error) {
	resp, err := c.do(ctx, http.MethodGet, publishPath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	return unmarshalPublication(id, resp.body)
}

// Unpublish removes a publication. The documentation calls this DELETE
// idempotent, but the sandbox answers a repeated one with 404
// ERROR_RESOURCE_NOT_FOUND (observed 2026-09-29), which matches ErrNotFound.
func (c *Client) Unpublish(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, publishPath+"/"+url.PathEscape(id), nil)
	return err
}

// CreateContact creates a contact address and returns its id.
func (c *Client) CreateContact(ctx context.Context, doc *contactDocument) (string, error) {
	body, err := marshalContact(doc)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, contactPath, body)
	if err != nil {
		return "", err
	}
	id, detail := createdID(resp, scoutID)
	if id == "" {
		return "", fmt.Errorf("the API accepted the contact (HTTP %d) but the provider could not determine its id (%s). "+
			"The contact probably exists in the account now and has to be removed or imported by hand", resp.status, detail)
	}
	return id, nil
}

// GetContact retrieves a contact address by id.
func (c *Client) GetContact(ctx context.Context, id string) (*contactDocument, error) {
	resp, err := c.do(ctx, http.MethodGet, contactPath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	return unmarshalContact(id, resp.body)
}

// UpdateContact replaces a contact address. PUT is a full replacement
// (observed 2026-09-29): an element that doc leaves out is cleared, except
// defaultContact, which keeps its value when left out.
func (c *Client) UpdateContact(ctx context.Context, id string, doc *contactDocument) error {
	body, err := marshalContact(doc)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPut, contactPath+"/"+url.PathEscape(id), body)
	return err
}

// DeleteContact deletes a contact address. The API moves the listings that
// use it to the default contact, and refuses to delete the default contact
// itself with an error that matches ErrDefaultContact.
func (c *Client) DeleteContact(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, contactPath+"/"+url.PathEscape(id), nil)
	return err
}

type response struct {
	status   int
	location string
	body     []byte
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building %s %s: %w", method, path, err)
	}
	// Basic Principles: reads send Accept, writes send Accept and Content-Type.
	req.Header.Set("Accept", mediaTypeXML)
	if body != nil {
		req.Header.Set("Content-Type", mediaTypeXML)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	tflog.Debug(ctx, "ImmobilienScout24 API request", map[string]any{"method": method, "path": path})
	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			// url.Error repeats the full URL; keep only the cause.
			err = urlErr.Err
		}
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}
	tflog.Debug(ctx, "ImmobilienScout24 API response", map[string]any{"method": method, "path": path, "status": httpResp.StatusCode})

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return nil, newAPIError(method, path, httpResp.StatusCode, respBody)
	}
	return &response{status: httpResp.StatusCode, location: httpResp.Header.Get("Location"), body: respBody}, nil
}

func newAPIError(method, path string, status int, body []byte) *APIError {
	apiErr := &APIError{Method: method, Path: path, StatusCode: status}
	if msgs, err := parseMessages(body); err == nil && len(msgs) > 0 {
		apiErr.Messages = msgs
		return apiErr
	}
	// Some errors (429, gateway errors) come back as plain text.
	text := strings.TrimSpace(string(body))
	if len(text) > maxErrorBodyBytes {
		text = strings.ToValidUTF8(text[:maxErrorBodyBytes], "") + "... (truncated)"
	}
	apiErr.Body = text
	return apiErr
}

func parseMessages(body []byte) ([]Message, error) {
	var doc messagesDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	return doc.Messages, nil
}

var (
	createdIDText        = regexp.MustCompile(`with id \[([^\]]+)\]`)
	scoutID              = regexp.MustCompile(`^\d+$`)
	publicationIDPattern = regexp.MustCompile(`^\d+_\d+$`)
)

// createdID extracts the id of a new resource from a create response; valid
// matches a well-formed id. The documented body carries it in <message><id>;
// the Responses page also promises a Location header, and the message text
// repeats the id, so both are fallbacks. Without an id, detail says why.
func createdID(resp *response, valid *regexp.Regexp) (id, detail string) {
	msgs, parseErr := parseMessages(resp.body)
	for _, m := range msgs {
		if m.Code == codeResourceCreated && valid.MatchString(strings.TrimSpace(m.ID)) {
			return strings.TrimSpace(m.ID), ""
		}
	}
	if resp.location != "" {
		if u, err := url.Parse(resp.location); err == nil {
			segment := u.Path[strings.LastIndex(u.Path, "/")+1:]
			if valid.MatchString(segment) {
				return segment, ""
			}
		}
	}
	for _, m := range msgs {
		if m.Code == codeResourceCreated {
			if match := createdIDText.FindStringSubmatch(m.Text); match != nil && valid.MatchString(match[1]) {
				return match[1], ""
			}
		}
	}
	if parseErr != nil {
		return "", "response is not a <common:messages> document: " + parseErr.Error()
	}
	return "", "no id in the response body or Location header"
}
