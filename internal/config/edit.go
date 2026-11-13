// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"gopkg.in/yaml.v3"
)

// AddProfileKeys adds or overwrites key/value pairs under profiles.name in
// the YAML document existing holds, creating the top level "profiles"
// map, name's own map, or the document itself, whichever is missing, and
// preserving every comment and the rest of the document's structure
// otherwise. existing may be empty, in which case a fresh document is
// built.
//
// This is the file a person edits by hand, not one gitcd owns and
// regenerates, so the edit goes through gopkg.in/yaml.v3's comment
// preserving Node tree rather than a decode into a plain struct followed
// by a full re-encode, which would silently drop every comment the file
// carried.
func AddProfileKeys(existing []byte, name string, keys map[string]string) ([]byte, error) {
	doc, err := parseOrEmptyDocument(existing)
	if err != nil {
		return nil, err
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config: the existing configuration is not a YAML mapping at its top level")
	}

	profiles := mappingChild(root, "profiles")
	profile := mappingChild(profiles, name)
	for _, key := range sortedStringKeys(keys) {
		setScalarChild(profile, key, keys[key])
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("config: render the updated configuration: %w", err)
	}
	return out, nil
}

// ErrMultipleDocuments is wrapped into the error AddProfileKeys returns
// when the existing configuration holds more than one YAML document:
// gopkg.in/yaml.v3 decodes and re-encodes only the first one, which would
// silently drop every document after it from the file this command
// rewrites.
var ErrMultipleDocuments = errors.New("config: the existing configuration holds more than one YAML document")

// parseOrEmptyDocument parses existing as a single YAML document node, or
// returns a fresh document holding an empty mapping when existing is
// empty: a configuration file that does not exist yet, or exists but is
// blank, is not a parse error, matching LoadFrom's own treatment of a
// missing file.
//
// existing holding more than one YAML document is refused with
// ErrMultipleDocuments rather than edited: this package's own
// yaml.v3.Node round trip, like config.LoadFrom, only ever sees the
// first document, so silently writing back just that one would discard
// every document after it with nothing printed to say so. A person who
// deliberately split the file this way is expected to combine it into
// one document by hand before running a command that edits it.
func parseOrEmptyDocument(existing []byte) (*yaml.Node, error) {
	if len(existing) == 0 {
		return &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}, nil
	}

	dec := yaml.NewDecoder(bytes.NewReader(existing))

	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: parse the existing configuration: %w", err)
	}

	if err := nextDocumentExists(dec); err != nil {
		return nil, err
	}

	if doc.Kind == 0 || len(doc.Content) == 0 {
		// A document holding only comments, or nothing at all, decodes
		// into a node with no content: the same empty case as a missing
		// file, just already having gone through the parser.
		return &yaml.Node{
			Kind:        yaml.DocumentNode,
			HeadComment: doc.HeadComment,
			Content:     []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}, nil
	}
	return &doc, nil
}

// nextDocumentExists reports, as ErrMultipleDocuments, whether dec still
// holds a second YAML document after the one parseOrEmptyDocument already
// decoded. A trailing, empty document, the shape a lone "---" end marker
// with nothing meaningful after it decodes to, is not treated as a real
// second document: only one that itself carries content counts.
func nextDocumentExists(dec *yaml.Decoder) error {
	var next yaml.Node
	err := dec.Decode(&next)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("config: parse the existing configuration: %w", err)
	case isEmptyDocument(&next):
		return nil
	default:
		return ErrMultipleDocuments
	}
}

// isEmptyDocument reports whether doc, a yaml.Node decoded as a whole
// document, carries no real content: either no document node at all, or
// a document node wrapping nothing but an implicit "!!null" scalar, the
// shape a bare "---" document separator with nothing meaningful after it
// decodes to.
func isEmptyDocument(doc *yaml.Node) bool {
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return true
	}
	content := doc.Content[0]
	return content.Kind == yaml.ScalarNode && content.Tag == "!!null"
}

// errNotFound is returned internally by mappingLookup when key is not
// among parent's children; mappingChild and setScalarChild both treat it
// as "create one" rather than an error, so it never escapes this file.
var errNotFound = errors.New("config: key not found in mapping")

// mappingChild returns parent's child mapping node keyed name, appending
// both the key and a fresh, empty mapping value when parent does not
// already carry one.
func mappingChild(parent *yaml.Node, name string) *yaml.Node {
	if value, err := mappingLookup(parent, name); err == nil {
		return value
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, key, value)
	return value
}

// setScalarChild sets parent's child scalar value keyed key to value,
// updating an existing entry's value node in place so any style or
// comment that particular node already carries is kept, or appending a
// freshly built key/value pair when parent carries no such key yet.
func setScalarChild(parent *yaml.Node, key, value string) {
	if existing, err := mappingLookup(parent, key); err == nil {
		existing.SetString(value)
		return
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{}
	valueNode.SetString(value)
	parent.Content = append(parent.Content, keyNode, valueNode)
}

// mappingLookup returns parent's value node keyed key, or errNotFound
// when parent, a mapping node, carries no such key.
func mappingLookup(parent *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1], nil
		}
	}
	return nil, errNotFound
}

// sortedStringKeys returns m's keys sorted, so a caller writing them into
// a document a person reads gets a deterministic order.
func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
