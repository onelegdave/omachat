import pathlib
import re
import unittest

REPO = pathlib.Path(__file__).resolve().parents[1]


def blocks(source, opener):
    """Yield the top-level lines of each `opener {` block."""
    lines = source.split("\n")
    pattern = re.compile(r"^\s*" + opener + r"\s*\{\s*$")
    for i, line in enumerate(lines):
        if not pattern.match(line):
            continue
        depth, own = 1, []
        for following in lines[i + 1:]:
            plain = re.sub(r'"(?:\\.|[^"\\])*"', '""', following)
            if depth == 1:
                own.append(following)
            depth += plain.count("{") - plain.count("}")
            if depth <= 0:
                break
        yield i + 1, own


class RemoteContentTest(unittest.TestCase):
    """The panel runs inside the shared shell process, so remote content must
    never reach a Qt decoder or rich-text loader there."""

    def test_every_text_declares_its_format(self):
        # Text defaults to AutoText, which renders a remote name or preview
        # holding <img src="https://..."> as rich text and fetches the image
        # inside the shell.
        styled = []
        for path in REPO.glob("*.qml"):
            for line, own in blocks(path.read_text(), "Text"):
                formats = [l.strip() for l in own if re.search(r"(^|[;\s])textFormat\s*:", l)]
                self.assertTrue(formats, f"{path.name}:{line} has no textFormat")
                if not all("Text.PlainText" in f and "StyledText" not in f for f in formats):
                    styled.append((path.name, own))
        # The only rich text is the message body, escaped by Model.linkify.
        self.assertEqual([name for name, _ in styled], ["InboxView.qml"])
        self.assertIn("id: bubbleText", "\n".join(styled[0][1]))
        inbox = (REPO / "InboxView.qml").read_text()
        body = inbox[inbox.index("id: bubbleText"):inbox.index("textFormat: row.msg && row.msg.deleted")]
        self.assertIn("return Model.linkify(", body)

    def test_no_in_process_media_playback(self):
        for path in REPO.glob("*.qml"):
            source = path.read_text()
            self.assertNotIn("QtMultimedia", source, path.name)
            self.assertIsNone(re.search(r"\b(MediaPlayer|Video|VideoOutput)\s*\{", source), path.name)
        self.assertFalse((REPO / "LoopingVideoThumb.qml").exists())

    def test_message_thumbnails_decode_helper_previews_only(self):
        inbox = (REPO / "InboxView.qml").read_text()
        thumb = inbox[inbox.index("id: thumb"):]
        thumb = thumb[:thumb.index("onLoadFailed")]
        self.assertIn("path: parent.loadsInline ? parent.previewPath", thumb)
        self.assertNotIn("mediaPath :", thumb.replace("mediaPath ||", ""))
        self.assertIn('mimeType: ""', thumb)
        # The helper sends remote content only to panels carrying this marker.
        service = (REPO / "Service.qml").read_text()
        self.assertIn('var frame = { id: id, method: method, network: net, client: "safe-content-1" }', service)
        wire = (REPO / "internal/wire/wire.go").read_text()
        self.assertIn('const SafeClient = "safe-content-1"', wire)
        composer = inbox[inbox.index("id: attachPreview"):]
        composer = composer[:composer.index("onClicked")]
        self.assertIn("root.pendingPreviews[root.pendingAttachment]", composer)

    def test_unconfirmed_helpers_get_no_content_requests(self):
        # A helper from before 0.6.0 sends original avatars. Until buildInfo
        # confirms helper previews, the panel sends only these requests.
        service = (REPO / "Service.qml").read_text()
        allowed = re.search(r"readonly property var contentFreeMethods: \[([^\]]*)\]", service).group(1)
        allowed = set(re.findall(r'"([A-Za-z]+)"', allowed))
        self.assertEqual(allowed, {"buildInfo", "status", "config", "setEnabledServices", "setNotifications", "setUiScale",
                                   "setChatView", "setWindowPreferences", "setKeyboardShortcuts", "setEmojiSearchPreference",
                                   "setUnifiedInboxPreference", "setWhatsAppHistoryCache", "setTelegramCredentials",
                                   "discardCapture", "unpair"})
        call = service[service.index("  function call(method"):]
        self.assertLess(call.index("contentFreeMethods.indexOf(method) < 0"), call.index("_write(frame)"))
        self.assertIn('res.contentProtocol === "safe-content-1"', service)
        self.assertIn('if (!contentAllowed && frame.event !== "status" && frame.event !== "config") return', service)
        server = (REPO / "internal/daemon/server.go").read_text()
        self.assertIn('"contentProtocol": wire.SafeClient', server)

    def test_copied_text_never_enters_argv(self):
        for path in REPO.glob("*.qml"):
            self.assertIsNone(re.search(r'"wl-copy"\s*,\s*"--"\s*,', path.read_text()), path.name)
        inbox = (REPO / "InboxView.qml").read_text()
        copy = inbox[inbox.index("function copyText"):]
        copy = copy[:copy.index("flashCopied()")]
        self.assertIn('copyProc.command = ["wl-copy", "--type", "text/plain;charset=utf-8"]', copy)
        self.assertNotIn("execArgv", copy)

    def test_shell_models_are_bounded(self):
        service = (REPO / "Service.qml").read_text()
        self.assertIn("readonly property int maxConversations: 1000", service)
        model = (REPO / "Model.js").read_text()
        self.assertIn("var MAX_CONVERSATIONS = 1000", model)
        for suffix in ("", "WA", "TG", "FB", "SG"):
            self.assertIn(f"root.conversations{suffix} = root._boundConversations(list)", service)
        inbox = (REPO / "InboxView.qml").read_text()
        display = inbox[inbox.index("function displayMessages"):]
        self.assertIn("messages = Model.boundThread(next)", display[:display.index("grouped =")])
        self.assertIn("readonly property int maxMediaEntries: 300", inbox)
        self.assertIn('(mediaRequests[key] === "external" && mediaPaths[key])', inbox)
        older = inbox[inbox.index("function loadOlderMessages"):]
        self.assertLess(older.index("Model.MAX_THREAD_MESSAGES"), older.index('source.call("messages"'))

    def test_gif_style_clips_animate_only_through_the_helper(self):
        inbox = (REPO / "InboxView.qml").read_text()
        self.assertIn("readonly property bool loadsInline: isImage || isVideoGif", inbox)
        self.assertIn("if (mediaAnimations[key]) params.animation = true", inbox)
        helper = (REPO / "internal/daemon/animation.go").read_text()
        # ffmpeg runs in its own process with a forced demuxer and file-only
        # input, and its output is re-encoded before the panel sees it.
        for flag in ('"-protocol_whitelist", "file"', '"-f", format, "-i", "file:"+src', '"-max_pixels"', '"-filter_threads", "2"', "trim=end_frame=", 'exec.CommandContext(ctx, prlimit,'):
            self.assertIn(flag, helper)
        preview = (REPO / "internal/daemon/preview.go").read_text()
        self.assertIn("data, err = p.runFFmpeg(job.src)", preview)
        self.assertIn("safeimage.Convert(data, job.edge)", preview)


if __name__ == "__main__":
    unittest.main()
