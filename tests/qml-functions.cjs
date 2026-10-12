// Execute the actual QML JavaScript functions without opening a desktop window.
// This checks state machines and callbacks; Qt layout, focus and signals still
// need the separately held QML integration checks.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
exports.load = function(file, names, values) {
  const source = fs.readFileSync(path.join(__dirname, '..', file), 'utf8');
  const ctx = vm.createContext(values);
  if (!ctx.Keybindings) {
    const keyboard = vm.createContext({});
    vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "Keybindings.js"), "utf8").replace(".pragma library", ""), keyboard);
    ctx.Keybindings = keyboard;
  }
  ctx.root = ctx;
  for (const name of names) {
    const match = source.match(new RegExp('^  function ' + name + '\\([^\\n]*\\) \\{[\\s\\S]*?^  \\}', 'm'));
    if (!match) throw new Error('Missing QML function ' + name);
    vm.runInContext(match[0], ctx);
  }
  return ctx;
};
