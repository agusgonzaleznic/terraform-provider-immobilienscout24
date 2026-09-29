package immobilienscout24

// A minimal reader for the live ImmobilienScout24 XSD, used by the tests and
// the fake API to derive element order from the schema itself instead of from
// a hand-written list.
//
// testdata/offer-v1.0-wadl-schemas.xml is the file the API documentation links
// as "XSD schemas", downloaded unmodified on 2026-09-29 from
// https://rest.immobilienscout24.de/restapi/api/offer/v1.0/?_wadl&_schema.xml

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

const (
	xsdNamespace    = "http://www.w3.org/2001/XMLSchema"
	commonNamespace = "http://rest.immobilienscout24.de/schema/common/1.0"
)

type xsdNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []xsdNode  `xml:",any"`
}

func (n *xsdNode) attr(local string) string {
	for _, a := range n.Attrs {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (n *xsdNode) is(local string) bool {
	return n.XMLName.Space == xsdNamespace && n.XMLName.Local == local
}

// xsdElement is one element of a flattened xs:sequence or xs:all.
type xsdElement struct {
	Name     string
	Type     string // the type attribute as written, e.g. "xs:double"
	Optional bool
	Multiple bool
}

type xsdType struct {
	node     *xsdNode
	prefixes map[string]string
	tns      string
}

type xsdSchemas struct {
	types map[string]xsdType // key: namespace + " " + name
}

var (
	loadXSDOnce sync.Once
	loadedXSD   *xsdSchemas
	loadXSDErr  error
)

func liveXSD(t testing.TB) *xsdSchemas {
	t.Helper()
	loadXSDOnce.Do(func() {
		raw, err := os.ReadFile("testdata/offer-v1.0-wadl-schemas.xml")
		if err != nil {
			loadXSDErr = err
			return
		}
		var root xsdNode
		if err := xml.Unmarshal(raw, &root); err != nil {
			loadXSDErr = err
			return
		}
		s := &xsdSchemas{types: map[string]xsdType{}}
		var walk func(n *xsdNode)
		walk = func(n *xsdNode) {
			if n.is("schema") {
				prefixes := map[string]string{}
				for _, a := range n.Attrs {
					if a.Name.Space == "xmlns" {
						prefixes[a.Name.Local] = a.Value
					}
				}
				tns := n.attr("targetNamespace")
				for i := range n.Children {
					c := &n.Children[i]
					if c.is("complexType") && c.attr("name") != "" {
						s.types[tns+" "+c.attr("name")] = xsdType{node: c, prefixes: prefixes, tns: tns}
					}
				}
				return
			}
			for i := range n.Children {
				walk(&n.Children[i])
			}
		}
		walk(&root)
		loadedXSD = s
	})
	if loadXSDErr != nil {
		t.Fatalf("loading the live XSD fixture: %v", loadXSDErr)
	}
	return loadedXSD
}

// elements returns the elements of a complex type, including every base type
// it extends, in document order.
func (s *xsdSchemas) elements(namespace, name string) ([]xsdElement, error) {
	typ, ok := s.types[namespace+" "+name]
	if !ok {
		return nil, fmt.Errorf("complex type {%s}%s not in the XSD", namespace, name)
	}
	var out []xsdElement
	body := typ.node
	for i := range typ.node.Children {
		cc := &typ.node.Children[i]
		if !cc.is("complexContent") {
			continue
		}
		for j := range cc.Children {
			ext := &cc.Children[j]
			if !ext.is("extension") {
				continue
			}
			baseNS, baseName := typ.resolve(ext.attr("base"))
			base, err := s.elements(baseNS, baseName)
			if err != nil {
				return nil, err
			}
			out = append(out, base...)
			body = ext
		}
	}
	for i := range body.Children {
		group := &body.Children[i]
		if !group.is("sequence") && !group.is("all") {
			continue
		}
		for j := range group.Children {
			e := &group.Children[j]
			if !e.is("element") {
				continue
			}
			name := e.attr("name")
			if ref := e.attr("ref"); name == "" && ref != "" {
				_, name = typ.resolve(ref)
			}
			out = append(out, xsdElement{
				Name:     name,
				Type:     e.attr("type"),
				Optional: e.attr("minOccurs") == "0",
				Multiple: e.attr("maxOccurs") != "" && e.attr("maxOccurs") != "1",
			})
		}
	}
	return out, nil
}

func (t xsdType) resolve(qname string) (string, string) {
	prefix, local, ok := strings.Cut(qname, ":")
	if !ok {
		return t.tns, qname
	}
	return t.prefixes[prefix], local
}

func mustElements(t testing.TB, namespace, name string) []xsdElement {
	t.Helper()
	els, err := liveXSD(t).elements(namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	return els
}

// checkOrder reports the first child that is unknown to, or out of order in,
// a sequence, and the first required element that is missing.
func checkOrder(context string, children []string, allowed []xsdElement) error {
	index := map[string]int{}
	for i, e := range allowed {
		index[e.Name] = i
	}
	last := -1
	seen := map[string]bool{}
	for _, c := range children {
		i, ok := index[c]
		if !ok {
			return fmt.Errorf("%s: element <%s> is not in the XSD sequence", context, c)
		}
		if i < last || (i == last && !allowed[i].Multiple) {
			return fmt.Errorf("%s: element <%s> is out of XSD order (after <%s>)", context, c, allowed[last].Name)
		}
		last = i
		seen[c] = true
	}
	for _, e := range allowed {
		if !e.Optional && !seen[e.Name] {
			return fmt.Errorf("%s: required element <%s> is missing", context, e.Name)
		}
	}
	return nil
}

func TestLiveXSDApartmentRentSequence(t *testing.T) {
	els := mustElements(t, realEstatesNamespace, "ApartmentRent")
	var names []string
	var required []string
	for _, e := range els {
		names = append(names, e.Name)
		if !e.Optional {
			required = append(required, e.Name)
		}
	}
	// Spot checks against section 3b of the contract research, so that a
	// broken walker cannot silently produce an empty or truncated order.
	if len(els) != 64 {
		t.Errorf("flattened ApartmentRent has %d elements, want 64: %v", len(els), names)
	}
	if got, want := strings.Join(required, ","), "title,address,showAddress,baseRent,livingSpace,courtage"; got != want {
		t.Errorf("required elements = %s, want %s", got, want)
	}
	if names[0] != "externalId" || names[len(names)-1] != "courtage" {
		t.Errorf("sequence starts with %s and ends with %s", names[0], names[len(names)-1])
	}
}
