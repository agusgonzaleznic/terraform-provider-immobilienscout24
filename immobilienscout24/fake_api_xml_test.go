package immobilienscout24

// Parsing requests and writing responses: the XML helpers that the resources
// of the fake API in fake_api_test.go share.

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// parseRequest parses a request body whose root is {namespace}local and whose
// other elements are unqualified, as the XSD declares them.
func parseRequest(body []byte, namespace, local string) (*xnode, error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var root *xnode
	var stack []*xnode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if tok.Name.Space != namespace || tok.Name.Local != local {
					return nil, fmt.Errorf("root element is {%s}%s, want {%s}%s", tok.Name.Space, tok.Name.Local, namespace, local)
				}
			} else if tok.Name.Space != "" {
				return nil, fmt.Errorf("element <%s> is in namespace %q, the XSD declares unqualified elements", tok.Name.Local, tok.Name.Space)
			}
			n := &xnode{Name: tok.Name.Local}
			for _, a := range tok.Attr {
				if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					n.Attrs = append(n.Attrs, a)
				}
			}
			if len(stack) == 0 {
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(tok)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty body")
	}
	return root, nil
}

func childNames(n *xnode) []string {
	var names []string
	for _, c := range n.Children {
		names = append(names, c.Name)
	}
	return names
}

func writeNode(b *strings.Builder, n *xnode) {
	b.WriteString("<" + n.Name)
	for _, a := range n.Attrs {
		b.WriteString(" " + a.Name.Local + `="`)
		_ = xml.EscapeText(b, []byte(a.Value))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	if len(n.Children) == 0 {
		_ = xml.EscapeText(b, []byte(n.Text))
	}
	for _, c := range n.Children {
		writeNode(b, c)
	}
	b.WriteString("</" + n.Name + ">")
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml;charset=UTF-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// writeMessages writes a <common:messages> body in the documented error shape.
func writeMessages(w http.ResponseWriter, status int, code, text string) {
	var b strings.Builder
	b.WriteString(`<common:messages xmlns:common="http://rest.immobilienscout24.de/schema/common/1.0"><message><messageCode>` + code + `</messageCode><message>`)
	_ = xml.EscapeText(&b, []byte(text))
	b.WriteString(`</message></message></common:messages>`)
	writeRaw(w, status, b.String())
}
