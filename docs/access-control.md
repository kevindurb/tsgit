# Access control

Authorization has exactly one source: the Tailscale app capability attached to
the peer's node by the tailnet policy. `tsgit` has no permission store, no local
policy file, and no account table.

## Capability name

```
github.com/kevindurb/tsgit/cap/repo
```

This name is a public contract. It is fixed before anyone writes policy;
changing it invalidates every grant in the tailnet.

## Payload

The capability value is a JSON array of grant objects. Each object grants one
`access` level over a list of repository patterns:

```json
[
  {"repos": ["acme/*"], "access": "write"},
  {"repos": ["acme/admin"], "access": "admin"},
  {"repos": ["shared/docs"], "access": "read"}
]
```

<!-- capability-fixture -->
```json
[
  {"repos": ["acme/*"], "access": "write"},
  {"repos": ["acme/admin"], "access": "admin"},
  {"repos": ["shared/docs"], "access": "read"}
]
```

| Field | Type | Meaning |
|---|---|---|
| `repos` | array of strings | Patterns matched against the canonical name |
| `access` | `read`, `write`, or `admin` | Highest level this entry grants |

Access levels are ordered `read < write < admin`. `write` implies `read`, and
`admin` implies `write`. Only `admin` authorizes deleting a repository.

An unknown `access` value, an entry that is not an object, or any other shape
that cannot be decoded makes the whole payload grant nothing. The failure is
logged and no partial grant is applied.

Adding `admin` changes the payload contract. An older `tsgit` binary cannot
decode it and fails the whole payload closed, so a peer granted `admin` loses
all repository access after a downgrade. Remove `admin` entries before rolling
back to a version that does not know the level.

## Canonical names

A repository name is exactly `<namespace>/<repo>`, two segments. The single
trailing `.git` in a URL is not part of the name, so `/acme/widget` and
`/acme/widget.git` both map to `acme/widget`. Nested namespaces are not
supported in this slice.

Patterns use Go's `path.Match` rules. `*` does not cross `/`, so `acme/*`
matches `acme/widget` but not `other/widget` and not `acme/team/widget`.
Grants union: if several entries match, the highest level wins. `write` implies
`read` and authorizes creating a missing repository; `admin` implies `write` and
authorizes deleting one. No matching entry denies.

## A complete grant

App capabilities only apply to traffic the device can already reach, so a
working grant needs both `ip` and `app`:

```json
"grants": [
  {
    "src": ["group:devs", "tag:ci"],
    "dst": ["tag:tsgit"],
    "ip": ["tcp:443"],
    "app": {
      "github.com/kevindurb/tsgit/cap/repo": [
        {"repos": ["acme/*"], "access": "write"},
        {"repos": ["shared/docs"], "access": "read"}
      ]
    }
  },
  {
    "src": ["group:admins"],
    "dst": ["tag:tsgit"],
    "ip": ["tcp:443"],
    "app": {
      "github.com/kevindurb/tsgit/cap/repo": [
        {"repos": ["acme/admin"], "access": "admin"}
      ]
    }
  }
]
```

Keep `admin` on a separate, narrower source than the write or CI grant, so a
write-only identity never receives deletion authority.

An `app`-only grant produces a capability nobody can reach.

## Public read capability

Public access has its own node-scoped capability:

```
github.com/kevindurb/tsgit/cap/funnel
```

Unlike `cap/repo`, this one is a **node attribute**, not a grant. It belongs
under `nodeAttrs.app` on the node that runs `tsgit`:

```json
"nodeAttrs": [{
  "target": ["tag:tsgit"],
  "attr": ["funnel"],
  "app": {
    "github.com/kevindurb/tsgit/cap/funnel": [
      {"repos": ["public/*", "shared/docs"]}
    ]
  }
}]
```

The payload is an array of objects containing only `repos` patterns:

<!-- funnel-capability-fixture -->
```json
[
  {"repos": ["public/*", "shared/docs"]}
]
```

| Field | Type | Meaning |
|---|---|---|
| `repos` | array of strings | Patterns matched against the canonical name |

There is no `access` field: public access is read-only by construction. An
unknown field, including an `access` copied from a `cap/repo` grant, makes the
entry invalid, so the whole payload grants nothing and the error is logged.
A missing payload means nothing is public. The payload is re-read on every
public request, so a policy edit takes effect without a restart.

A `cap/funnel` payload under `grants[].app` has **no public effect**. Grant
capabilities are attached to the source peer's capability map; an anonymous
Funnel client has no peer and no `WhoIs` response, so there is nothing to
attach them to. Only the node's own `nodeAttrs.app` is consulted. The `funnel`
attribute alone only enables the transport; it does not publish anything.

## Tagged machines

Authorization uses the node's identity, not a user. A machine carrying
`tag:ci` can hold a capability and read or push with no user login attached.
This is deliberate: CI runners are the main case where the machine, not a
person, is the principal.

## Policy changes

The capability is read from the same `WhoIs` response that identifies the
peer, on every request. Removing a grant from the tailnet policy takes effect on
the next request with no `tsgit` restart and no on-disk change. A fetch already
in flight keeps its earlier decision; there is no mid-transfer check.
