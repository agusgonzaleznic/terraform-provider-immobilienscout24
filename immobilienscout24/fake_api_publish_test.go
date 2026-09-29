package immobilienscout24

// The publish resource of the fake API in fake_api_test.go. It mirrors what
// the live sandbox did on 2026-09-29, including where that contradicts the
// documentation:
//
//   - POST /offer/v1.0/publish answers 201 with a Location header and a
//     messages body that carries the new id; 409 ERROR_COMMON_REQUEST_CONFLICT
//     when the listing is already published on the channel; 404
//     ERROR_COMMON_RESOURCE_NOT_FOUND when the real estate does not exist;
//   - GET /offer/v1.0/publish/{id} answers the publishObject, or 404
//     ERROR_COMMON_RESOURCE_NOT_FOUND;
//   - DELETE /offer/v1.0/publish/{id} answers 200 MESSAGE_RESOURCE_DELETED, and
//     404 ERROR_RESOURCE_NOT_FOUND when repeated: it is not idempotent,
//     although the documentation says so;
//   - deleting a real estate deletes its publications;
//   - the real estate GET carries realEstateState, ACTIVE while the listing is
//     published on 10000 and INACTIVE otherwise, whatever 10001 does, and a
//     publishChannel element per publication.
//
// Simulated: the documentation asks to send publish requests "one after the
// other and not in parallel", but not what happens otherwise. The fake holds
// every publish request for fakePublishDelay and fails all that overlap, so
// that a provider which publishes in parallel fails the acceptance tests.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	fakePublishPath = "/restapi/api/offer/v1.0/publish"
	// fakePublishDelay is long enough for two publish requests that Terraform
	// sends in parallel to overlap reliably.
	fakePublishDelay = 150 * time.Millisecond
	// fakeOverlapError starts the message of a publish request that
	// overlapped another one.
	fakeOverlapError = "fake API: publish requests overlapped"
)

// fakePublishChannels are the channels GET publishchannel listed for the
// sandbox account, with their titles.
var fakePublishChannels = map[string]string{"10000": "ImmobilienScout24", "10001": "Homepage"}

type fakePublication struct {
	realEstateID string
	channelID    string
}

// fakePublishState is the publish side of fakeAPI, guarded by fakeAPI.mu.
type fakePublishState struct {
	publications map[string]fakePublication // by publication id
	// published and removedOutOfBand hold, once per event, the ids of the
	// publications created through the API and of those removed out of band.
	published        []string
	removedOutOfBand []string
	// inFlight and starts detect publish requests in parallel.
	inFlight, starts int
	// conflictNext answers that many upcoming publish requests with a bare 409
	// conflict and no publication, as the API may for concurrent access.
	conflictNext int
}

// PublicationIDs returns the ids of every publication, sorted.
func (f *fakeAPI) PublicationIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id := range f.pub.publications {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// PublishHistory returns the ids of the publications created through the API
// and of those removed out of band, once per event.
func (f *fakeAPI) PublishHistory() (published, removedOutOfBand []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pub.published...), append([]string(nil), f.pub.removedOutOfBand...)
}

// ConflictNextPublish makes the next publish request fail with a bare 409
// conflict, without creating or finding a publication.
func (f *fakeAPI) ConflictNextPublish() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pub.conflictNext++
}

// PublishOutOfBand publishes a real estate as if someone did it on the website.
func (f *fakeAPI) PublishOutOfBand(realEstateID, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pub.publications[publicationID(realEstateID, channelID)] = fakePublication{realEstateID: realEstateID, channelID: channelID}
}

// UnpublishOutOfBand removes a publication as if someone did it on the website.
func (f *fakeAPI) UnpublishOutOfBand(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.pub.publications[id]; ok {
		delete(f.pub.publications, id)
		f.pub.removedOutOfBand = append(f.pub.removedOutOfBand, id)
	}
}

// removePublications deletes the publications of a real estate and returns
// their ids. The caller holds f.mu.
func (f *fakeAPI) removePublications(realEstateID string) []string {
	var removed []string
	for id, p := range f.pub.publications {
		if p.realEstateID == realEstateID {
			delete(f.pub.publications, id)
			removed = append(removed, id)
		}
	}
	return removed
}

func (f *fakeAPI) handlePublish(w http.ResponseWriter, method, urlPath string, body []byte) {
	id, isItem := strings.CutPrefix(urlPath, fakePublishPath+"/")
	isItem = isItem && id != "" && !strings.Contains(id, "/")
	switch {
	case urlPath == fakePublishPath && method == http.MethodPost:
		f.publish(w, body)
	case isItem && method == http.MethodGet:
		f.getPublication(w, id)
	case isItem && method == http.MethodDelete:
		f.unpublish(w, id)
	default:
		writeMessages(w, http.StatusMethodNotAllowed, "ERROR_COMMON_METHOD_NOT_ALLOWED", "Method not allowed.")
	}
}

func (f *fakeAPI) publish(w http.ResponseWriter, body []byte) {
	f.mu.Lock()
	f.pub.inFlight++
	f.pub.starts++
	start, overlapped := f.pub.starts, f.pub.inFlight > 1
	f.mu.Unlock()
	time.Sleep(fakePublishDelay)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.pub.inFlight--
	// Another publish request was in flight when this one arrived, or arrived
	// while this one was held. Both fail.
	if overlapped || f.pub.starts != start {
		writeMessages(w, http.StatusBadRequest, "ERROR_COMMON_BAD_REQUEST",
			fakeOverlapError+". The documentation asks to send them one after the other and not in parallel.")
		return
	}

	if f.pub.conflictNext > 0 {
		f.pub.conflictNext--
		writeMessages(w, http.StatusConflict, "ERROR_COMMON_REQUEST_CONFLICT",
			"The operation for your request causes a conflict.")
		return
	}

	realEstateID, channelID, err := parsePublishRequest(body)
	if err != nil {
		writeMessages(w, http.StatusPreconditionFailed, "ERROR_COMMON_SCHEMA_VALIDATION_FAILED", err.Error())
		return
	}
	if _, ok := f.objects[realEstateID]; !ok {
		writeMessages(w, http.StatusNotFound, "ERROR_COMMON_RESOURCE_NOT_FOUND", "Resource was not found.")
		return
	}
	if _, ok := fakePublishChannels[channelID]; !ok {
		// Simulated: the account can only use the channels GET publishchannel lists.
		writeMessages(w, http.StatusPreconditionFailed, "ERROR_RESOURCE_VALIDATION", "fake API: publish channel "+channelID+" is not available.")
		return
	}
	id := publicationID(realEstateID, channelID)
	if _, exists := f.pub.publications[id]; exists {
		writeMessages(w, http.StatusConflict, "ERROR_COMMON_REQUEST_CONFLICT",
			"The operation for your request causes a conflict. [MESSAGE: Publish object already exists for publishchannel id="+
				channelID+", realestate id="+realEstateID+"]")
		return
	}
	f.pub.publications[id] = fakePublication{realEstateID: realEstateID, channelID: channelID}
	f.pub.published = append(f.pub.published, id)
	w.Header().Set("Location", f.BaseURL()+"/offer/v1.0/publish/"+id)
	writeRaw(w, http.StatusCreated, fakePublishCreatedBody(id))
}

func (f *fakeAPI) getPublication(w http.ResponseWriter, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pub.publications[id]
	if !ok {
		writeMessages(w, http.StatusNotFound, "ERROR_COMMON_RESOURCE_NOT_FOUND", "Resource was not found.")
		return
	}
	title := ""
	if obj := f.objects[p.realEstateID]; obj != nil && obj.child("title") != nil {
		title = obj.child("title").Text
	}
	writeRaw(w, http.StatusOK, fakePublishObjectBody(p.realEstateID, title, p.channelID))
}

func (f *fakeAPI) unpublish(w http.ResponseWriter, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.pub.publications[id]; !ok {
		// The code is the sandbox's; the text is simulated.
		writeMessages(w, http.StatusNotFound, "ERROR_RESOURCE_NOT_FOUND", "Resource [publish] with id ["+id+"] not found.")
		return
	}
	delete(f.pub.publications, id)
	// Verbatim from the Delete a publication page.
	writeRaw(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?>
<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" xmlns:xlink="http://www.w3.org/1999/xlink">
  <message>
    <messageCode>MESSAGE_RESOURCE_DELETED</messageCode>
    <message>Resource [NAME] with id [ID] has been deleted.</message>
  </message>
</common:messages>`)
}

// parsePublishRequest checks a publish request against common:PublishObject:
// a common:publishObject root holding realEstate and then publishChannel,
// both unqualified, empty and with a numeric id.
func parsePublishRequest(body []byte) (string, string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var children []xml.StartElement
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", fmt.Errorf("malformed XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			switch depth {
			case 0:
				if tok.Name.Space != commonNamespace || tok.Name.Local != "publishObject" {
					return "", "", fmt.Errorf("root element is {%s}%s, want {%s}publishObject", tok.Name.Space, tok.Name.Local, commonNamespace)
				}
			case 1:
				children = append(children, tok.Copy())
			default:
				return "", "", fmt.Errorf("unexpected element <%s> inside <%s>", tok.Name.Local, children[len(children)-1].Name.Local)
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	var names []string
	for _, c := range children {
		names = append(names, c.Name.Local)
	}
	if strings.Join(names, ",") != "realEstate,publishChannel" {
		return "", "", fmt.Errorf("publishObject: want <realEstate> and then <publishChannel>, got %v", names)
	}
	ids := make([]string, len(children))
	for i, c := range children {
		if c.Name.Space != "" {
			return "", "", fmt.Errorf("element <%s> is in namespace %q, the XSD declares unqualified elements", c.Name.Local, c.Name.Space)
		}
		for _, a := range c.Attr {
			if a.Name.Space == "" && a.Name.Local == "id" {
				ids[i] = a.Value
			}
		}
		if !isDigits(ids[i]) {
			return "", "", fmt.Errorf("<%s> needs a numeric id attribute, got %q", c.Name.Local, ids[i])
		}
	}
	return ids[0], ids[1], nil
}

// realEstateState is what the real estate GET reports: ACTIVE while the
// listing is published on 10000, INACTIVE otherwise, also while it is still
// published on 10001. The caller holds f.mu.
func (f *fakeAPI) realEstateState(realEstateID string) string {
	if _, ok := f.pub.publications[publicationID(realEstateID, "10000")]; ok {
		return "ACTIVE"
	}
	return "INACTIVE"
}

// writePublishChannels writes the common:publishChannels element of a real
// estate GET, with a publishChannel per publication. Simulated: whether an
// unpublished listing carries an empty element was not observed; the fake
// leaves it out. The caller holds f.mu.
func (f *fakeAPI) writePublishChannels(b *strings.Builder, realEstateID string) {
	var channels []string
	for _, p := range f.pub.publications {
		if p.realEstateID == realEstateID {
			channels = append(channels, p.channelID)
		}
	}
	if len(channels) == 0 {
		return
	}
	sort.Strings(channels)
	list := &xnode{Name: "common:publishChannels"}
	for _, c := range channels {
		list.Children = append(list.Children, &xnode{Name: "publishChannel", Attrs: []xml.Attr{
			{Name: xml.Name{Local: "id"}, Value: c},
			{Name: xml.Name{Local: "title"}, Value: fakePublishChannels[c]},
		}})
	}
	writeNode(b, list)
}

// observedNamespaces are the namespace declarations on every publish response
// of the sandbox.
const observedNamespaces = `xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0" ` +
	`xmlns:gis="http://rest.immobilienscout24.de/schema/platform/gis/1.0" ` +
	`xmlns:search="http://rest.immobilienscout24.de/schema/search/common/1.0" ` +
	`xmlns:ns5="http://rest.immobilienscout24.de/schema/search/shortlist/1.0" ` +
	`xmlns:xlink="http://www.w3.org/1999/xlink"`

// fakePublishCreatedBody is the sandbox's 201 body for a publish request.
func fakePublishCreatedBody(id string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:messages ` + observedNamespaces + `>
    <message>
        <messageCode>MESSAGE_RESOURCE_CREATED</messageCode>
        <message>Resource [publish] with id [` + id + `] has been created.</message>
        <id>` + id + `</id>
    </message>
</common:messages>`
}

// fakePublishObjectBody is the sandbox's body for GET publish/{id}; title is
// the title of the real estate. Simulated: firstActivationDate is fixed, and
// was only observed for a publication on 10000.
func fakePublishObjectBody(realEstateID, title, channelID string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(title))
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<common:publishObject ` + observedNamespaces + ` id="` + publicationID(realEstateID, channelID) + `">
    <realEstate id="` + realEstateID + `" title="` + escaped.String() + `" firstActivationDate="2026-09-29T22:16:58.000+02:00"/>
    <publishChannel id="` + channelID + `" title="` + fakePublishChannels[channelID] + `"/>
</common:publishObject>`
}
