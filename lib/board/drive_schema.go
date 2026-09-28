package board

// The mounted-drive capability schemas. They are published verbatim in the
// device descriptor so a host can advertise the exact action contract, and the
// provider enforces the same limits in Go. Descriptions mention only the granted
// root: the host filesystem path behind it is not part of the action contract.

const driveListInputSchema = `{
  "type": "object",
  "description": "List entries inside the granted root.",
  "properties": {
    "path": {
      "type": "string",
      "description": "Directory relative to the granted root, using forward slashes. Omit or use \".\" for the root itself. Absolute paths and \"..\" are refused."
    },
    "recursive": {
      "type": "boolean",
      "description": "Include the children of subdirectories."
    },
    "limit": {
      "type": "integer",
      "minimum": 0,
      "description": "Maximum entries to return. Clamped to the provider bound; cannot be raised."
    }
  },
  "additionalProperties": false
}`

const driveListingOutputSchema = `{
  "type": "object",
  "description": "One level of a directory listing inside the granted root.",
  "properties": {
    "path": {
      "type": "string",
      "description": "Listed directory relative to the granted root, using forward slashes."
    },
    "entries": {
      "type": "array",
      "description": "Entries sorted by name within each directory level.",
      "items": {
        "type": "object",
        "properties": {
          "path": {
            "type": "string",
            "description": "Entry path relative to the granted root, using forward slashes."
          },
          "name": { "type": "string", "description": "Entry name as it appears in its directory." },
          "kind": { "type": "string", "enum": ["file", "directory"] },
          "size": { "type": "integer", "minimum": 0, "description": "Bytes, or 0 for a directory." },
          "modTime": { "type": "string", "format": "date-time" }
        },
        "required": ["path", "name", "kind", "size", "modTime"],
        "additionalProperties": false
      }
    },
    "truncated": {
      "type": "boolean",
      "description": "True when the entry or scan limit ended the listing early."
    },
    "skipped": {
      "type": "integer",
      "minimum": 0,
      "description": "Children withheld because they resolve outside the granted root or could not be inspected."
    }
  },
  "required": ["path", "entries", "truncated", "skipped"],
  "additionalProperties": false
}`

const driveReadInputSchema = `{
  "type": "object",
  "description": "Read one file inside the granted root as a bounded content stream.",
  "properties": {
    "path": {
      "type": "string",
      "description": "File relative to the granted root, using forward slashes. Absolute paths and \"..\" are refused."
    },
    "maxBytes": {
      "type": "integer",
      "minimum": 0,
      "description": "Tighten the provider's read bound for this action. Cannot raise it."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`

// The read output describes the stream, not the bytes. File contents are
// delivered through Connection.Stream so that Result.Output stays small enough
// to log, compare, and forward.
const driveReadOutputSchema = `{
  "type": "object",
  "description": "Stream bounds for one drive.read. The bytes themselves are delivered through Connection.Stream, never inside a Result.",
  "properties": {
    "size": {
      "type": "integer",
      "minimum": 0,
      "description": "Total bytes the content stream will deliver."
    },
    "truncated": {
      "type": "boolean",
      "description": "True when the file is larger than the bound this stream enforces."
    }
  },
  "required": ["size", "truncated"],
  "additionalProperties": false
}`
