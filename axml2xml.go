package axml2xml

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"regexp"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	TAG_OPEN              uint32 = 0x10
	TAG_SUPPORTS_CHILDREN uint32 = 0x100000
	TAG_TEXT              uint32 = 0x08
)

type Tag struct {
	name     string
	flags    uint32
	attrs    []map[string]string
	children []*Tag
}

func DecompressXML(r io.Reader) (xml string, err error) {
	var br *bufio.Reader
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	pos := 0 // file position
	// Some header, seems to be 3000 8000 always.
	var v uint32
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Total file length.
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Unknown, always 0100 1c00
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Seems to be related to the total length of the string table.
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Number of items in the string table, plus some header non-sense?
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	strnum := int(v)
	// Seems to always be 0.
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Seems to always be 1.
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// No clue, relates to the size of the string table?
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	// Seems to always be 0.
	if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
		return "", err
	}
	pos += 9 * 4
	// Offset in string table of each string.
	stroffs := make([]int, strnum)
	for i := 0; i < strnum; i++ {
		if err = binary.Read(br, binary.LittleEndian, &v); err != nil {
			return "", err
		}
		pos += 4
		stroffs[i] = int(v)
	}
	strs := make(map[int]string)
	curroffs := 0
	// the string table looks to have been serialized from a hash table, since
	// the positions are not sorted :)
	sort.Ints(stroffs)
	for _, offs := range stroffs {
		if offs != curroffs {
			return "", fmt.Errorf("Invaild string offset=%#v", offs)
		}
		var l, length uint16
		if err = binary.Read(br, binary.LittleEndian, &l); err != nil {
			return "", err
		}
		buf := make([]byte, l*2)
		if _, err = io.ReadFull(br, buf); err != nil {
			return "", err
		}
		// Read the NUL, we're not interested in storing it.
		if err = binary.Read(br, binary.LittleEndian, &length); err != nil {
			return "", err
		}
		strs[offs] = utf16BytesToString(buf, binary.LittleEndian)
		curroffs += (int(l)+1)*2 + 2
	}
	pos += curroffs
	strings := make([]string, len(strs))
	idx := 0
	for _, offs := range stroffs { // keep the order
		strings[idx] = strs[offs]
		idx++
	}

	// OPEN TAG:
	// V=tagS 0x1400 1400 V=1 V=0 {ATTR? V=7 V=attrS V=valS V=attrS|0x3<<24 V=valS 0x0301 1000 V=0x18 V=? } V=~0 V=~0
	// V=1    0x0800 0000 V=0x19 0x0201 1000 V=0x38 V=7 V=~0 V=~0
	//
	// OPEN TAG (normal, child, 3 attributes):
	// V=tagS 0x1400 1400 V=3 V=0 V=xmlns V=attrS V=valS 0x0800 0010 V=~0 V=xmlns V=attrS V=valS V=0x0800 0010 V=~0 V=xmlns V=attrS V=valS 0x0800 0003 V=valS 0x0301 1000 V=0x18? V=0x0b? V=~0 V=~0
	//
	// OPEN TAG (outer tag, no attributes):
	// V=tagS 0x1400 1400 V=0    V=0         0x0401 1000 V=0x1c V=0    V=~0
	// V=1    0x0800 0000 V=0x20 0x0201 1000 V=0x38      V=0x4  V=~0   V=~0
	//
	// OPEN TAG (normal, child, NO ATTRIBUTES):
	// V=tagS 0x1400 1400 V=0    V=0         0x0301 1000 V=0x18 V=0x0b V=~0 V=~0
	//
	// CLOSE TAG (normal, child):
	// V=tagS 0x0401 1000 V=0x1c V=0         V=~0
	//
	// CLOSE TAG (outer tag):
	// V=tagS 0x0101 1000 V=0x18 V=0x0c      V=~0

	// Looks like the string table is word-aligned.
	for i := 0; i < pos%4; i++ {
		if _, err = br.ReadByte(); err != nil {
			return "", err
		}
	}

	//my $no_clue1 = read_doc($doc, 48);

	if err = readPastSentinel(br, 0); err != nil {
		return "", err
	}

	//my $nstag = unpack('V', read_doc($doc, 4));
	//my $nsurl = unpack('V', read_doc($doc, 4));
	//
	//my $nsmap = { $nsurl => $nstag };
	//my $nstags = { reverse %$nsmap };
	//
	//my $nsdummy = read_doc($doc, 20);

	parsed, err := readMeat(br, strings)
	if err != nil {
		return "", err
	}
	return parsed, nil
}

func (node *Tag) printTree(depth int) string {
	var buff bytes.Buffer
	for i := 0; i < depth; i++ {
		buff.WriteString("\t")
	}
	if node.flags&TAG_TEXT != 0 {
		buff.WriteString(node.name)
		return buff.String()
	}
	buff.WriteString("<")
	if node.flags&TAG_OPEN == 0 {
		buff.WriteString("/")
	}
	buff.WriteString(node.name)
	for _, attr := range node.attrs {
		buff.WriteString(" ")
		if ns, ok := attr["ns"]; ok {
			buff.WriteString(fmt.Sprintf("%s:", ns))
		}
		buff.WriteString(fmt.Sprintf("%s=\"%s\"", attr["name"], attr["value"]))
	}
	if len(node.children) == 0 {
		buff.WriteString(" /")
	}
	buff.WriteString(">\n")
	if len(node.children) > 0 {
		for _, child := range node.children {
			buff.WriteString(child.printTree(depth + 1))
		}
		for i := 0; i < depth; i++ {
			buff.WriteString("\t")
		}
		buff.WriteString(fmt.Sprintf("</%s>\n", node.name))
	}
	return buff.String()
}

func readMeat(br *bufio.Reader, strings []string) (parsed string, err error) {
	nsmap := make(map[uint32]uint32)
	root := new(Tag)
	if err = readTag(br, strings, nsmap, root); err != nil {
		return
	}
	if root.children, err = readChildren(br, strings, nsmap, root.name); err != nil {
		return
	}
	return root.printTree(0), nil
}

func readChildren(br *bufio.Reader, strings []string,
	nsmap map[uint32]uint32,
	stoptag string) (children []*Tag, err error) {
	for {
		tag := new(Tag)
		if err = readTag(br, strings, nsmap, tag); err != nil {
			return
		}
		if (tag.flags & TAG_SUPPORTS_CHILDREN) != 0 {
			if (tag.flags & TAG_OPEN) != 0 {
				if tag.children, err = readChildren(br, strings, nsmap, tag.name); err != nil {
					return
				}
			} else if tag.name == stoptag {
				break
			}
		}
		children = append(children, tag)
	}
	return
}

func readTag(br *bufio.Reader, strings []string,
	nsmap map[uint32]uint32, tag *Tag) error {
	xmlns := make([]map[string]string, 0, 8)
	slen := len(strings)
	ns_pattern := regexp.MustCompile(`(?i)^[a-z]+$`)
	url_pattern := regexp.MustCompile(`^http://`)
	var unknown uint32
	// Hack to support the strange xmlns attribute encoding without disrupting our
	// processor.
READ_AGAIN:
	var name, flags uint32
	if err := binary.Read(br, binary.LittleEndian, &name); err != nil {
		return err
	}
	if err := binary.Read(br, binary.LittleEndian, &flags); err != nil {
		return err
	}
	// Strange way to specify xmlns attribute.
	if int(name) < slen && int(flags) < slen {
		ns := strings[name]
		url := strings[flags]

		// TODO: How do we expect this?
		ns_matched := ns_pattern.MatchString(ns)
		url_matched := url_pattern.MatchString(url)
		if ns_matched && url_matched {
			nsmap[flags] = name
			xmlns = append(xmlns, map[string]string{
				"name":  fmt.Sprintf("xmlns:%s", ns),
				"value": url,
			})
			readPastSentinel(br, 0)
			goto READ_AGAIN
		}
	}
	if (flags&TAG_SUPPORTS_CHILDREN) != 0 && (flags&TAG_OPEN) != 0 {
		var attrs, ns uint32
		var attr, value, attrflags uint32
		if err := binary.Read(br, binary.LittleEndian, &attrs); err != nil {
			return err
		}
		if err := binary.Read(br, binary.LittleEndian, &unknown); err != nil {
			return err
		}

		for ; attrs > 0; attrs-- {
			if err := binary.Read(br, binary.LittleEndian, &ns); err != nil {
				return err
			}
			if err := binary.Read(br, binary.LittleEndian, &attr); err != nil {
				return err
			}
			// TODO: Escaping?
			if err := binary.Read(br, binary.LittleEndian, &value); err != nil {
				return err
			}
			if err := binary.Read(br, binary.LittleEndian, &attrflags); err != nil {
				return err
			}
			if value == 0xffffffff { // -1, last index of array
				value = uint32(slen) - 1
			}
			var attr_map = map[string]string{
				"name":  strings[attr],
				"value": strings[value],
				// "flag": strconv.Itoa(int(attrflags)),
			}
			if ns != 0xffffffff {
				attr_map["ns"] = strings[nsmap[ns]]
			}

			xmlns = append(xmlns, attr_map)
			// padding
			if err := binary.Read(br, binary.LittleEndian, &unknown); err != nil {
				return err
			}
			// readPastSentinel(br, 1);
		}

		readPastSentinel(br, 0)
	} else {
		// There is strong evidence here that what I originally thought
		// to be a sentinel is not ;)
		if err := binary.Read(br, binary.LittleEndian, &unknown); err != nil {
			return err
		}
		if err := binary.Read(br, binary.LittleEndian, &unknown); err != nil {
			return err
		}

		readPastSentinel(br, 0)
	}

	tag.name = strings[name]
	tag.flags = flags
	tag.attrs = xmlns

	return nil
}

func readPastSentinel(r *bufio.Reader, count int) (err error) {
	// Read to sentinel.
	var v uint32
	pos := 0
	for v != 0xFFFFFFFF {
		if err = binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		pos += 4
	}

	n := 1
	// Read past it.
	if count == 0 {
		for {
			b, err := r.Peek(4)
			if err != nil {
				return err
			}
			if !(b[0] == 0xFF && b[1] == 0xFF && b[2] == 0xFF && b[3] == 0xFF) {
				break
			}
			if err = binary.Read(r, binary.LittleEndian, &v); err != nil {
				return err
			}
			pos += 4
			n++
			if count >= n {
				break
			}
		}
	}
	// skipped $n sentinels, $pos bytes
	return nil
}

func utf16BytesToString(b []byte, o binary.ByteOrder) string {
	utf := make([]uint16, (len(b)+(2-1))/2)
	for i := 0; i+(2-1) < len(b); i += 2 {
		utf[i/2] = o.Uint16(b[i:])
	}
	if len(b)/2 < len(utf) {
		utf[len(utf)-1] = utf8.RuneError
	}
	return string(utf16.Decode(utf))
}
