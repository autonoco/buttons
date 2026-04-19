package drawer

import _ "embed"

//go:generate go run ../tools/schemagen -public ../../docs/schemas/drawer.schema.json -embed ./schema_embedded.json

// SchemaJSON is the JSON Schema document describing drawer.json.
// Generated from Go structs via internal/tools/schemagen and embedded
// so the CLI can serve it without a separate download (`buttons drawer
// schema` prints this byte-for-byte). The canonical copy lives at
// docs/schemas/drawer.schema.json and is published via SchemaStore.
//
//go:embed schema_embedded.json
var SchemaJSON []byte
