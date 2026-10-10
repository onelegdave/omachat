# Keyboard control and offline emoji search

PR #9 supplies the action registry, editable shortcuts, searchable action menu,
and bundled emoji metadata. The local integration keeps four supported services;
Signal and conversation creation are separate candidates. Original contribution
commits retain Matthias MB's authorship and their source commit references.

Bare navigation keys act outside text fields. Modified actions remain available
while editing; shortcut recording and modal pickers own their input. Escape
closes the innermost picker before leaving editing or closing OmaChat. A restored
writable chat keeps composer focus; an unselected inbox starts at its list.
Tab and arrows retain standard control navigation. Ctrl+Tab cycles visible tabs;
Ctrl+number selects their current position, including All when it is enabled.

Settings saves and configuration reads reject replies from a retired service or
socket lifetime. Keyboard action names, conflict validation, and defaults match
between QML and the helper. Unsupported services/actions are excluded.

Emoji search uses native glyphs, English names, keywords, and aliases, with
bounded fuzzy matching. Its text clears by default and can be retained through
an explicit preference. The dataset and license provenance are documented in
[emoji-data.md](emoji-data.md). Generation is manual from a pinned local archive;
the helper and picker do not download metadata or install packages.

Node tests exercise the actual JavaScript modules and QML state functions.
Offscreen fixtures exercise real InboxView controls with fictional messages.
Native anchored-panel, pop-out, focus/IME behavior, and visual rendering require
isolated Wayland acceptance before release. Local checks are not marketplace
approval or live provider acceptance.
