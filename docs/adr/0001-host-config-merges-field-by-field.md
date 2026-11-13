# 1. A host config entry merges field by field onto its default

Status: accepted

## Context

- `config.Default()` ships `dev.azure.com` with both a `layout` and `aliases`, because
  `remote.Parse` recognises Azure DevOps unconditionally and needs a layout carrying the
  project segment.
- A user's own `hosts` entry for the same host is meant to override one field, aliases for
  example, without having to repeat every other field the default carries.
- `gopkg.in/yaml.v3` decoding into a plain struct cannot tell a key the file leaves out from
  one set to its zero value: both decode to `""`, `nil` or `false`.
- `LoadFrom` must still reject an unknown key, so the decode target stays a `KnownFields`
  struct, not a generic map.

## Decision

A host entry decodes into a struct of optional fields (`*string` for `layout` and
`protocol`, `[]string` for `aliases`) and is merged field by field onto the default entry for
that host, rather than replacing it outright.

- `yaml.v3` already tells a left out key apart from one set to its zero value: an absent
  `layout:` or `protocol:` key decodes to a `nil` pointer, and an absent `aliases:` key
  decodes to a `nil` slice, while `aliases: []` decodes to a non-nil, empty slice. No presence
  map or sentinel value is needed.
- A left out field keeps the default entry's value. A present field, even an explicit empty
  one, replaces it.
- `aliases: []` is how a user deliberately clears a default's aliases.
- `layout: ""` is how a user deliberately clears a default's layout; the cleared value then
  falls back to the generic `layout.default` through `Config.LayoutFor`, the same path an
  entry with no `layout` key at all already takes for a host with no default of its own.

## Consequences

- Adding one alias to `dev.azure.com` no longer drops its layout, and vice versa.
- A host with no default entry is unaffected: merging onto a zero value `Host` is the same as
  building one from scratch.
- `layout: ""` and an absent `layout` key end up rendering the same way, through the same
  fallback. The two are distinguishable in the decoded struct, but not in `LayoutFor`'s
  result, which is intentional: there is no behaviour left to give a dedicated empty value
  that omission does not already produce.
- The optional field struct duplicates `Host`'s shape. A future field on `Host` needs the same
  pointer, or nil-vs-empty-slice, treatment on its raw counterpart, or it silently loses this
  merge behaviour.

## Alternatives

- A presence map (`map[string]bool` recording which YAML keys were seen): tells omission and
  explicit zero apart for every field uniformly, but needs a second, hand maintained
  structure that must be kept in sync with every field `Host` gains, for no added behaviour
  over the pointer and nil-slice fields `yaml.v3` already supports natively.
- Decode into `map[string]any` and inspect keys directly: loses `KnownFields`, so an unknown
  key under a host would need its own rejection logic instead of yaml.v3's own.
- Treat every field as always replacing the default outright, undoing #6's fix instead of
  fixing the merge: reintroduces exactly the bug this ADR records the fix for.
