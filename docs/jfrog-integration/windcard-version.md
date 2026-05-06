# Flagging All Versions of a Package as Malicious

## Problem

When a backend sends `package@0` (meaning all versions are malicious), what we will put in `vulnerable_versions`?

Using a specific version like `["[1.0.4]"]` only flags that exact version.
Using `["0"]` or `["[0]"]` only flags version `0.0.0`.

## Solution

We will use the open-ended range notation `(,)` to match all versions:

```json
"components": [
  {
    "id": "veltrix",
    "vulnerable_versions": ["(,)"]
  }
]
```

## Version Range Cheat Sheet

| Use case                    | Format              | Handled |
|-----------------------------|---------------------|---------|
| Specific version            | `["[1.0.4]"]`       | Yes     |
| All versions                | `["(,)"]`           | Yes     |
| From version X onwards      | `["[1.0.0,)"]`      | NO      |
| Up to version X (exclusive) | `["(,2.0.0)"]`      | NO      |
| From X to Y (inclusive)     | `["[1.0.0,2.0.0]"]` | NO      |


Only [1] and [2] are handled since they are the only needed, from our backend also, we will have specific version or all versions (@0, wildcard)

## Malware ID in case of @0

We will use SD-MAL-{pkg-name}-ALL, i.e ALL for version.

## Mapping Rule

When backend sends `package@0` → use `"vulnerable_versions": ["(,)"]`
When backend sends `package@1.0.4` → use `"vulnerable_versions": ["[1.0.4]"]`