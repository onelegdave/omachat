# Offline emoji search data

## Provenance and redistribution

The bundled `data/emoji-search.json` is derived from the published npm archive
`emojibase-data@17.0.0`. Its registry metadata identifies git commit
`a5fc630a91ca42cddf3f4a66492965600fd3bce8`; the archive and individual input
files are SHA-256 pinned in `scripts/generate-emoji-data.py` and
`data/emoji-search.sources.json`.[1]

English labels/tags and the `github` and `joypixels` shortcode presets are
imported. Emojibase documents GitHub as its official-API-derived preset and
`discord` as an alias of `joypixels`, explicitly warning that Discord compatibility
may be inaccurate.[2] The pinned generator obtains GitHub names from the emoji
API, retaining Unicode entries and excluding custom platform artwork.[6]

Redistribution notices are bundled, not inferred solely from the npm license:

- **Emojibase:** MIT; copyright (c) 2017-2019 Miles Johnson. The unmodified
  package notice is `data/EMOJIBASE-LICENSE.txt`.[3]
- **Unicode character/emoji data:** Unicode License V3; the permission allows
  copying, modification and redistribution with its notice. The notice fetched
  from Unicode is in `data/UNICODE-LICENSE.txt`.[7]
- **CLDR annotations:** Unicode License V3; the CLDR release-48 license (linked
  by the pinned Emojibase documentation) is copied verbatim to
  `data/CLDR-LICENSE.txt`.[2][5]
- **JoyPixels / EmojiOne names:** the toolkit license explicitly places JSON
  and other **non-artwork** files under MIT, separately from restricted artwork.
  `data/JOYPIXELS-LICENSE.md` reproduces that declaration from toolkit 10.0.0
  commit `2614aecc77ea019c238cffd49830617031157583` and includes the MIT
  permission terms. Upstream does not supply a year-specific copyright notice
  in that file; none is fabricated here.[8][9]
- **GitHub aliases:** names only, from Emojibase's MIT-distributed preset; no
  GitHub PNGs, logos or custom emoji are copied. The pinned importer records
  their API origin, but does not pin the original GitHub API response. We pin
  the resulting published preset bytes, not an invented upstream snapshot.[3][6]

No JoyPixels, GitHub, or other third-party artwork is bundled. Rendering uses
native Unicode glyphs. These are **Discord-style** shortcodes, not an official
Discord dataset or a claim of complete Discord parity.[2][8]

Emojibase's upstream importer uses live alias sources when building its release;
we do not claim a separately verified original JoyPixels revision or GitHub
snapshot for those bytes. The published release's exact preset files are the
reproducible source of this bundle. Preserve all four notice files when
redistributing the dataset.

## Normalization and verified inventory

The manual generator:

1. Verifies the entire archive before reading a whitelist of members (without
   extracting tar paths).
2. Flattens nested skin variants, preserving each upstream `emoji` glyph and
   English `label`. Skin variants inherit their parent's tags when upstream
   omits them; this derivation is explicit, not additional claimed source data.
3. Excludes the 26 standalone regional-indicator letters lacking upstream
   browse order. Keeps flags, components, gender/ZWJ sequences and skin tones.
4. Merges alias identity ignoring FE0E/FE0F presentation selectors **only**;
   never removes ZWJ, gender or skin tone. Alternative labels on any duplicate
   identity become keywords. Original emitted glyphs are unchanged (e.g. the
   source thumbs-up glyph includes FE0F).
5. Retains Unicode browse order, sorts/deduplicates keyword and alias arrays,
   and emits UTF-8 JSON deterministically.

Verified generated inventory:

| Item | Count |
| --- | ---: |
| Source top-level records | 1,949 |
| Flattened records including skins | 3,979 |
| Excluded standalone regional indicators | 26 |
| Bundled distinct presentation-insensitive glyphs | 3,953 |
| GitHub alias identities included / source | 1,870 / 1,870 |
| JoyPixels alias identities included / source | 3,782 / 3,808 |
| Aliases across records, deduplicated within each record | 6,630 |

The 26 omitted JoyPixels identities are those same regional indicators, listed
in the manifest. Alias collisions across different glyphs remain searchable.

Output SHA-256:
`75590089f24896045ccadcc443063db98d2ad2345526f2ddb83bf3effb3cc241`.

Archive SHA-256:
`d01d0e2ca4e22cb402679532155342c71e2e871f25c32223c013b8b166ebdf5a`.

## Manual regeneration (stdlib only)

No runtime downloads, npm dependency, or package installation is needed. The
**manual** download is separate from the generator, which never opens a network
connection:

```sh
curl -fL https://registry.npmjs.org/emojibase-data/-/emojibase-data-17.0.0.tgz \
  -o /tmp/emojibase-data-17.0.0.tgz
python3 scripts/generate-emoji-data.py --archive /tmp/emojibase-data-17.0.0.tgz
python3 scripts/generate-emoji-data.py --archive /tmp/emojibase-data-17.0.0.tgz --check
python3 scripts/generate-emoji-data.py --check
node --test tests/model-emoji-search.test.cjs
```

Generation writes only `emoji-search.json`, `emoji-search.sources.json`, and the
Emojibase notice to `data/` (or `--output-dir`). The archive-backed `--check`
reproduces and compares exact bytes; `--check` alone validates the bundled
schema, identity uniqueness, record count and dataset/Emojibase-license hashes.
The other notices are reviewed copies, not regenerated from the npm archive.

## Integration contract

`EmojiSearch.js` is a QML `.pragma library` with pure functions and no Qt, Node,
filesystem, network, global mutable cache or package dependencies:

```js
var index = EmojiSearch.buildIndex(rows) // build once after loading data
var results = EmojiSearch.search(index, query) // original row objects
```

The index is opaque; retain it until the input data changes. The module does not
mutate rows. Records have `{ e, label, keywords: [], aliases: [] }`; old
`{ e, k }` rows also work for the UI's existing fallback. No Omarchy dataset is
copied into this bundle; existing fallback vocabulary remains searchable via
`k`. Loading/fallback handling remains the integrating QML caller's responsibility.

Search lowercases and trims surrounding colons/whitespace; underscores,
hyphens, internal colons and spaces become word separators. `+1` and `-1`
remain distinct. Pasted glyph matching ignores presentation selectors while
returning the exact original glyph. Empty queries browse everything in input
order; no match returns an empty array.

Ranking is exact alias/name/glyph, then whole-field prefix, token, substring,
then one-edit Damerau matches (including adjacent transposition). Fuzzy
alias/name matches precede fuzzy keyword-token matches. Multiword queries
require every word, allow reordered words across fields, and rank by the worst
matched tier. Ties preserve input order explicitly. Fuzzy expansion applies
only to alphanumeric words of 4–64 characters, never tiny query words, glyphs
or signed aliases. Literal short queries still match prefixes/substrings.

The independent Node suite loads the QML JS in `vm` after stripping only its
pragma. It verifies search behavior, real source vocabulary, glyph uniqueness,
source notices and checksums, and benchmarks 70 warmed searches over all 3,953
records. The p95 regression ceiling is 200 ms; measured timings are emitted in
the test output rather than asserted as a universal hardware guarantee. UI
integration/focus/insertion/reaction tests are outside this module's scope.


## Sources

[1] https://registry.npmjs.org/emojibase-data/17.0.0
[2] https://raw.githubusercontent.com/milesj/emojibase/a5fc630a91ca42cddf3f4a66492965600fd3bce8/website/docs/shortcodes.md
[3] https://raw.githubusercontent.com/milesj/emojibase/a5fc630a91ca42cddf3f4a66492965600fd3bce8/LICENSE
[5] https://raw.githubusercontent.com/unicode-org/cldr/release-48/LICENSE
[6] https://raw.githubusercontent.com/milesj/emojibase/a5fc630a91ca42cddf3f4a66492965600fd3bce8/packages/generator/src/generators/shortcodes/generateGitHub.ts
[7] https://www.unicode.org/license.txt
[8] https://raw.githubusercontent.com/joypixels/emoji-toolkit/2614aecc77ea019c238cffd49830617031157583/LICENSE.md
[9] https://opensource.org/license/mit
