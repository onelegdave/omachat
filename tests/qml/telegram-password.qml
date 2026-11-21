import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

// Telegram two-step verification: the pairing view asks for the cloud password
// once the helper reports passwordRequired, sends it only to submitCloudPassword,
// clears the field, and keeps the prompt open after a wrong password.
ShellRoot {
  id: root
  property int step: 0
  property int ticks: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  QtObject {
    id: fake
    property string state: "pairing"
    property var status: ({state: "pairing", passwordRequired: true, passwordHint: "first pet",
                           hint: "Two-step verification is on for this account. Enter your Telegram cloud password to finish signing in."})
    property var browserProfiles: []
    property var calls: []
    function statusFor(net) { return status }
    function stateFor(net) { return state }
    function call(method, params, callback, network) { calls.push({method: method, params: params, callback: callback, network: network}) }
    function loadProfiles() {}
  }
  FloatingWindow {
    implicitWidth: 500; implicitHeight: 700; visible: true
    Chat.PairingView { id: pairing; anchors.fill: parent; service: fake; network: "telegram" }
  }
  TestResult { id: inspect }
  Timer {
    interval: 100; running: true; repeat: true
    onTriggered: {
      try {
        if (++root.ticks > 60) throw new Error("fixture did not finish")
        var prompt = inspect.findChild(pairing, "cloudPasswordPrompt")
        var field = inspect.findChild(pairing, "cloudPasswordField")
        var hint = inspect.findChild(pairing, "cloudPasswordHint")
        var error = inspect.findChild(pairing, "cloudPasswordError")
        var call
        switch (root.step++) {
        case 0:
          check(pairing.passwordPrompt && prompt.visible, "the password prompt is shown while Telegram waits for it")
          check(!pairing.isQR, "the accepted QR code is not shown again")
          check(hint.visible && hint.text === "Hint: first pet" && hint.textFormat === Text.PlainText, "the account hint is shown as plain text")
          check(field.readOnly && field.echoMode === TextInput.Password, "the password field is masked and read-only to native input")
          pairing.submitCloudPassword()
          check(fake.calls.length === 0 && error.visible && error.text === "Enter your Telegram cloud password.", "an empty password is not sent")
          field.text = "wrong password"
          pairing.submitCloudPassword()
          check(fake.calls.length === 1, "one submission is sent")
          call = fake.calls[0]
          check(call.method === "submitCloudPassword" && call.network === "telegram" && call.params.password === "wrong password", "the password goes only to submitCloudPassword for Telegram")
          check(field.text === "" && pairing.passwordBusy && !error.visible, "the field clears and the prompt waits for the helper")
          pairing.submitCloudPassword()
          check(fake.calls.length === 1, "a second submission waits for the first")
          call.callback(false, "That password is incorrect. Try again.")
          check(!pairing.passwordBusy && pairing.passwordPrompt, "a wrong password keeps the prompt open")
          check(error.visible && error.text === "That password is incorrect. Try again.", "the helper's error is shown")
          break
        case 1:
          field.text = "right password"
          pairing.submitCloudPassword()
          check(fake.calls.length === 2 && fake.calls[1].params.password === "right password", "a retry is sent")
          fake.calls[1].callback(true, null)
          fake.state = "connected"
          fake.status = {state: "connected"}
          break
        case 2:
          check(!pairing.passwordPrompt && !prompt.visible, "the prompt closes once Telegram connects")
          check(field.text === "" && pairing.passwordError === "" && !pairing.passwordBusy, "no password or error remains after sign-in")
          fake.state = "pairing"
          fake.status = {state: "pairing", passwordRequired: true}
          break
        case 3:
          check(prompt.visible && !hint.visible, "a prompt without a hint shows no hint line")
          inspect.findChild(pairing, "cloudPasswordCancel").clicked()
          check(fake.calls.length === 3 && fake.calls[2].method === "unpair" && fake.calls[2].network === "telegram", "cancel ends the Telegram sign-in")
          console.log("OMACHAT_TELEGRAM_PASSWORD_PASS")
          stop()
          Qt.callLater(Qt.quit)
          break
        }
      } catch (e) {
        stop()
        console.error("OMACHAT_TELEGRAM_PASSWORD_FAIL", e)
        Qt.callLater(Qt.quit)
      }
    }
  }
}
