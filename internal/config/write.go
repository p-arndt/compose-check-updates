package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// configFileMode is the mode a config file ccu creates gets. It is a file the
// user edits by hand, so it is theirs to read and write and nobody else's to
// change.
const configFileMode fs.FileMode = 0o644

// SetImageMax records max as the cap for image in the config file at path,
// creating the file (and its directory) when it does not exist yet. An existing
// file is edited in place, keeping its comments, key order and formatting: it is
// a file the user writes by hand.
func SetImageMax(path, image string, max Level) error {
	// Refuse before touching the file rather than writing a value the next Parse
	// would reject: a config ccu itself wrote should never fail to load.
	if !max.Valid() {
		return fmt.Errorf("config: max: %q is not one of %s, %s, %s", max, LevelPatch, LevelMinor, LevelMajor)
	}

	return setImageKey(path, image, "max", string(max))
}

// SetImageVersioning records the scheme image's tags are read under in the
// config file at path, the same way SetImageMax records a cap. It exists so the
// answer to "ccu cannot read this image" can be given where the question is
// asked, instead of being a hand edit the user has to find their own way to.
func SetImageVersioning(path, image string, versioning Versioning) error {
	if err := ValidateVersioning(versioning); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	// An empty scheme is the absence of one, and absence is what clearing means:
	// writing it out would leave the file saying nothing in a way that still has
	// to be read.
	if versioning == "" {
		return ClearImageVersioning(path, image)
	}

	return setImageKey(path, image, "versioning", string(versioning))
}

// ClearImageMax removes the cap for image from the config file at path. Removing
// a cap that is not there is not an error. When the image's entry is left empty
// by the removal, the entry goes too, and so does an `images:` map left with no
// entries at all.
func ClearImageMax(path, image string) error { return clearImageKey(path, image, "max") }

// ClearImageVersioning removes the scheme recorded for image, leaving it to be
// read under whatever the run resolved as its default.
func ClearImageVersioning(path, image string) error {
	return clearImageKey(path, image, "versioning")
}

// setImageKey writes one key of one image's policy, creating the file (and its
// directory) when it does not exist yet. An existing file is edited in place,
// keeping its comments, key order and formatting: it is a file the user writes
// by hand, and a save that reformatted it would be a save they regret.
func setImageKey(path, image, key, value string) error {
	doc, mode, err := readDocument(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	root := documentRoot(doc)
	if root == nil {
		return fmt.Errorf("%s: config is not a mapping", path)
	}

	images := mappingValue(root, "images")
	if images == nil || images.Kind != yaml.MappingNode {
		// A key written as a bare `images:` parses as null; it holds no entries, so
		// there is nothing to lose by replacing it with the mapping one needs.
		images = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(root, "images", images)
	}

	entry := mappingValue(images, image)
	if entry == nil || entry.Kind != yaml.MappingNode {
		entry = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(images, image, entry)
	}

	if node := mappingValue(entry, key); node != nil && node.Kind == yaml.ScalarNode {
		// Patching the existing scalar keeps whatever style and comments the user
		// gave it; only the word changes.
		node.Value = value
		node.Tag = "!!str"
	} else {
		setMappingValue(entry, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}

	return writeDocument(path, doc, mode)
}

// clearImageKey removes one key of one image's policy. Removing what is not
// there is not an error. When the image's entry is left empty by the removal,
// the entry goes too, and so does an `images:` map left with no entries at all.
func clearImageKey(path, image, key string) error {
	doc, mode, err := readDocument(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	root := documentRoot(doc)
	if root == nil {
		return nil
	}

	images := mappingValue(root, "images")
	if images == nil || images.Kind != yaml.MappingNode {
		return nil
	}

	entry := mappingValue(images, image)
	if entry == nil {
		return nil
	}

	if !deleteMappingKey(entry, key) {
		// Nothing to clear, so nothing to rewrite: an untouched file cannot lose
		// formatting to a save that had no work to do.
		return nil
	}

	if entry.Kind != yaml.MappingNode || len(entry.Content) == 0 {
		deleteMappingKey(images, image)
	}
	if len(images.Content) == 0 {
		deleteMappingKey(root, "images")
	}

	return writeDocument(path, doc, mode)
}

// readDocument parses path into a node tree, along with the mode to write it
// back with. A missing or empty file yields an empty mapping document so the
// callers can patch the same shape either way.
func readDocument(path string) (*yaml.Node, fs.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyDocument(), configFileMode, os.ErrNotExist
	}
	if err != nil {
		return nil, 0, err
	}

	mode := configFileMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 {
		return emptyDocument(), mode, nil
	}
	return &doc, mode, nil
}

func emptyDocument() *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
	}
}

// documentRoot returns the mapping the config's top-level keys live in. A
// document holding anything but a mapping has no place to put them.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil
	}

	root := doc.Content[0]
	switch {
	case root.Kind == yaml.MappingNode:
		return root
	case root.Kind == yaml.ScalarNode && root.Tag == "!!null":
		// A file with only comments in it parses as null; turning it into a
		// mapping keeps those comments and gives the new key somewhere to go.
		*root = yaml.Node{
			Kind:        yaml.MappingNode,
			Tag:         "!!map",
			HeadComment: root.HeadComment,
			LineComment: root.LineComment,
			FootComment: root.FootComment,
		}
		return root
	}
	return nil
}

// mappingValue returns the value node stored under key, or nil when the mapping
// does not have that key.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMappingValue appends key to the mapping, or replaces the value it already
// holds. Appending rather than inserting is what keeps the user's key order:
// what ccu adds goes at the end, and what was there stays where it was written.
func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// deleteMappingKey drops a key and its value, reporting whether there was one.
func deleteMappingKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return true
		}
	}
	return false
}

// writeDocument encodes doc over path. The file is written beside its target and
// renamed into place, so an interrupted save cannot leave a half-replaced config.
// A document left with no keys at all is removed rather than written: see below.
func writeDocument(path string, doc *yaml.Node, mode fs.FileMode) error {
	if root := documentRoot(doc); root != nil && len(root.Content) == 0 {
		// Nothing left to save, and an empty file would still sit in the project
		// looking as though it configured something.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".ccu-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// GlobalWritePath returns the file SetImageMax should write for the global
// scope: the existing global config if there is one, otherwise the preferred
// location for a new one.
func GlobalWritePath() (string, error) {
	if p := globalFile(); p != "" {
		return p, nil
	}
	dirs := globalDirs()
	if len(dirs) == 0 {
		return "", errors.New("config: no home directory to write a global config to")
	}
	return filepath.Join(dirs[0], globalNames[0]), nil
}

// ProjectWritePath returns the file SetImageMax should write for the project
// scope: the existing project config if one was found at or above root,
// otherwise a new one in root itself.
func ProjectWritePath(root string) (string, error) {
	if p := findProjectFile(root); p != "" {
		return p, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, projectNames[0]), nil
}
