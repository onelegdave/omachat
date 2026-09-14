const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const rgba = (r,g,b,a=1) => ({r,g,b,a});
const model = vm.createContext({Qt:{rgba}});
vm.runInContext(fs.readFileSync(`${__dirname}/../Model.js`, 'utf8').replace('.pragma library',''), model);

test('body, metadata, and avatar ink remain readable across dark, light, and low-contrast palettes', () => {
  const surfaces = [rgba(0,0,0),rgba(1,1,1),rgba(.12,.13,.18),rgba(.5,.5,.5),rgba(.9,.8,.65)];
  const inks = [rgba(.05,.06,.1),rgba(.12,.12,.14),rgba(.5,.2,.3),rgba(.95,.94,.9),rgba(1,1,1,.25)];
  for (const surface of surfaces) for (const ink of inks) {
    assert.ok(model.contrastRatio(model.readableInk(surface,ink),surface)>=4.5);
    assert.ok(model.contrastRatio(model.metaInk(ink,surface),surface)>=4.5);
    assert.ok(model.contrastRatio(model.inkOn(surface,ink,surface),surface)>=4.5);
  }
});

test('readable theme colors retain their identity and poor colors get an opaque fallback', () => {
  const dark=rgba(.02,.02,.02), light=rgba(.9,.9,.9);
  assert.equal(model.readableInk(dark,light),light);
  assert.equal(model.readableInk(dark,rgba(.1,.1,.1,.4)).a,1);
});

test('message surfaces support readable body and metadata text across a palette sweep', () => {
  for(let i=0;i<=20;i++) for(let j=0;j<=20;j++) {
    const bg=rgba(i/20,i/20,i/20), fg=rgba(j/20,.18,.35), accent=rgba(.2,j/20,.6);
    for(const surface of [model.outgoingFill(bg,accent,fg),model.incomingFill(bg,fg)]) {
      const ink=model.inkOn(surface,fg,bg);
      assert.ok(model.contrastRatio(ink,surface)>=4.5);
      assert.ok(model.contrastRatio(model.metaInk(ink,surface),surface)>=4.5);
    }
  }
});
