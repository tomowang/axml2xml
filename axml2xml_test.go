package axml2xml

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
)

const (
	xmlFilename string = "AndroidManifest.xml"
)

func checkXml(xml string, t *testing.T) {
	xml = strings.ToLower(strings.TrimSpace(xml))

	if !strings.HasPrefix(xml, "<manifest ") {
		t.Fatalf("Bad mainfest header %#v", xml)
	}

	if !strings.HasSuffix(xml, "</manifest>") {
		t.Fatalf("Bad mainfest header %#v", xml)
	}
}

func TestDecompressXML(t *testing.T) {
	axml, err := ioutil.ReadFile(xmlFilename)
	if err != nil {
		t.Fatalf("ReadFile(%#v) error: %s", xmlFilename, err)
	}

	xml, err := DecompressXML(bytes.NewReader(axml))

	fmt.Print(xml)

	if err != nil {
		t.Fatalf("DecompressXML error: %s", err)
	}

	checkXml(xml, t)
}
