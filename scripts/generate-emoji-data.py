#!/usr/bin/env python3
"""Generate offline emoji metadata from a locally supplied, pinned npm archive.

No network, npm, or third-party Python dependencies. See docs/emoji-data.md.
"""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import tarfile

VERSION = "17.0.0"
COMMIT = "a5fc630a91ca42cddf3f4a66492965600fd3bce8"
ARCHIVE_URL = "https://registry.npmjs.org/emojibase-data/-/emojibase-data-17.0.0.tgz"
ARCHIVE_SHA256 = "d01d0e2ca4e22cb402679532155342c71e2e871f25c32223c013b8b166ebdf5a"
MEMBERS = {
    "package/en/data.json": "ed014f1049bd370c5794f815850156196ac382850f51c3e9f6a9e83553fb3f01",
    "package/en/shortcodes/github.json": "d6bb7101e7bab1e52b30f4b3562da2319b484a6266352a1119f0d1ba52db23db",
    "package/en/shortcodes/joypixels.json": "71c0608ffb9715b0bcd7dcc5001218fee07ddbd95635e328a392b9cc07a70ab8",
    "package/LICENSE": "5c4e1582dcae9429a5d00ec0a64185c3a711f9757b2308fdf275d90b120f6149",
}
ROOT = Path(__file__).resolve().parent.parent


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def encode(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def identity(hexcode):
    # Presentation selectors only: ZWJ, gender and skin tone remain distinct.
    return tuple(int(part, 16) for part in hexcode.split("-")
                 if int(part, 16) not in (0xFE0E, 0xFE0F))


def generate(archive):
    if sha256(archive.read_bytes()) != ARCHIVE_SHA256:
        raise ValueError("source archive SHA-256 mismatch")
    # Read only known members; never extract archive paths onto the filesystem.
    with tarfile.open(archive, "r:gz") as source:
        members = {}
        for name in MEMBERS:
            member = source.extractfile(name)
            if member is None:
                raise ValueError(f"source member is not a regular file: {name}")
            with member:
                members[name] = member.read()
    for name, data in members.items():
        if sha256(data) != MEMBERS[name]:
            raise ValueError(f"source member SHA-256 mismatch: {name}")
    source_rows = json.loads(members["package/en/data.json"])
    flat = []
    for base in source_rows:
        flat.append((base, base.get("tags", [])))
        flat.extend((skin, base.get("tags", [])) for skin in base.get("skins", []))
    # Exclude the 26 bare regional indicators, not Unicode emoji-test entries.
    flat = sorted((item for item in flat if "order" in item[0]),
                  key=lambda item: item[0]["order"])
    merged = {}
    for source_row, inherited_tags in flat:
        key = identity(source_row["hexcode"])
        tags = source_row.get("tags", inherited_tags)
        if key not in merged:
            merged[key] = {"e": source_row["emoji"], "label": source_row["label"],
                           "keywords": list(tags), "aliases": []}
        else:
            row = merged[key]
            row["keywords"].extend(tags + [source_row["label"]])
    preset_counts = {}
    for preset in ("github", "joypixels"):
        mapping = json.loads(members[f"package/en/shortcodes/{preset}.json"])
        included = 0
        unmatched = []
        for hexcode, names in mapping.items():
            key = identity(hexcode)
            if key not in merged:
                unmatched.append(hexcode)
                continue
            included += 1
            merged[key]["aliases"].extend([names] if isinstance(names, str) else names)
        preset_counts[preset] = {"source_identities": len(mapping),
                                 "included_identities": included,
                                 "excluded_hexcodes": unmatched}
    rows = list(merged.values())
    for row in rows:
        row["keywords"] = sorted(set(row["keywords"]))
        row["aliases"] = sorted(set(row["aliases"]))
    validate(rows)
    output = encode(rows)
    manifest = {
        "package": "emojibase-data", "version": VERSION, "git_commit": COMMIT,
        "archive_url": ARCHIVE_URL, "archive_sha256": ARCHIVE_SHA256,
        "members_sha256": MEMBERS, "source_top_level_records": len(source_rows),
        "source_flat_records": len(source_rows) + sum(len(row.get("skins", [])) for row in source_rows),
        "excluded_unordered_records": sum("order" not in row for row in source_rows),
        "presets": preset_counts, "output_records": len(rows),
        "output_aliases": sum(len(row["aliases"]) for row in rows),
        "output_sha256": sha256(output),
    }
    return output, encode(manifest), members["package/LICENSE"]


def validate(rows):
    if len(rows) != 3953:
        raise ValueError(f"expected 3953 records, got {len(rows)}")
    seen = set()
    for row in rows:
        if set(row) != {"e", "label", "keywords", "aliases"}:
            raise ValueError("unexpected record schema")
        if not isinstance(row["e"], str) or not row["e"] or not row["label"]:
            raise ValueError("missing glyph or label")
        row["e"].encode("utf-8", errors="strict")
        key = tuple(ord(c) for c in row["e"] if ord(c) not in (0xFE0E, 0xFE0F))
        if key in seen:
            raise ValueError("duplicate presentation-insensitive glyph")
        seen.add(key)
        for field in ("keywords", "aliases"):
            if not isinstance(row[field], list) or not all(isinstance(v, str) and v for v in row[field]):
                raise ValueError(f"invalid {field}")
            if len(set(row[field])) != len(row[field]):
                raise ValueError(f"duplicate {field}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, help="local pinned emojibase-data-17.0.0.tgz")
    parser.add_argument("--check", action="store_true", help="verify rather than write; with --archive also reproduce")
    parser.add_argument("--output-dir", type=Path, default=ROOT / "data")
    args = parser.parse_args()
    targets = (args.output_dir / "emoji-search.json", args.output_dir / "emoji-search.sources.json",
               args.output_dir / "EMOJIBASE-LICENSE.txt")
    if args.archive:
        generated = generate(args.archive)
        for target, content in zip(targets, generated):
            if args.check:
                if target.read_bytes() != content:
                    raise ValueError(f"generated output differs: {target}")
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(content)
    elif not args.check:
        parser.error("--archive is required to generate; downloads are deliberately manual")
    output = targets[0].read_bytes()
    rows = json.loads(output)
    validate(rows)
    manifest = json.loads(targets[1].read_bytes())
    if manifest["output_sha256"] != sha256(output):
        raise ValueError("vendored output SHA-256 mismatch")
    if manifest["output_records"] != len(rows):
        raise ValueError("manifest record count mismatch")
    if sha256(targets[2].read_bytes()) != MEMBERS["package/LICENSE"]:
        raise ValueError("Emojibase license SHA-256 mismatch")
    print(f"Verified {len(rows)} records; SHA-256 {sha256(output)}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError, tarfile.TarError) as error:
        print(f"emoji data: {error}", file=sys.stderr)
        sys.exit(1)
