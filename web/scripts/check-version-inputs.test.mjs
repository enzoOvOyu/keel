import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { inspectVersionInputs, inspectDynamicCatalog, checkTree } from './check-version-inputs.mjs'
const file = 'src/features/example/Launch.tsx'
const fails = text => inspectVersionInputs(file, text).failures.length > 0

test('blocks native/shared/aliased free text, version labels and handmade version selects', () => {
 for (const text of [
  '<input id="driver-version" />',
  '<Input value={firmwareVersion} />',
  '<Input {...{value: driverVersion}} />',
  `import * as UI from '@/components/ui/input'; const Form = () => <UI.Input value={driverVersion} />`,
  '<textarea name="os.version" />',
  '<Textarea onChange={setNcclVersion} />',
  '<div><Label>OS version</Label><Input value={target}/></div>',
  '<label>OS version<input value={target}/></label>',
  '<input aria-label="目标版本" />',
  '<NativeSelect id="tool.version" />',
  `import { Input as Field } from '@/components/ui/input'; const Form = () => <Field value={driverVersion} />`,
 ]) assert(fails(text), text)
})
test('permits ParamSelect, non-version controls, immutable revision selectors and readonly displays', () => {
 for (const text of [
  '<ParamSelect value={driverVersion} />',
  '<Input id="hostname" value={hostname} />',
  '<select id="execution-flow" value={flowVersionId} />',
  '<label>{t("flowBefore")}<select value={left}><option>{revision.version}</option></select></label>',
  '<Input value={driverVersion} readOnly />',
  '<Input value={driverVersion} readOnly={true} />',
 ]) assert(!fails(text), text)
 for (const value of ['false', 'busy']) assert(fails(`<Input value={driverVersion} readOnly={${value}} />`))
 assert(fails('<Input value={driverVersion} disabled={busy} />'))
 assert(fails('<Input value={driverVersion} readOnly {...props} />'))
 assert(fails('<select id="driver-version" readOnly />'))
})
test('media registration is an exact file, component and control exception', () => {
 const exception = { file, component: 'RegisterMediaDialog', id: 'media-version', kind: 'media-registration' }
 const text = 'function RegisterMediaDialog(){return <input id="media-version" value={version} />} '
 assert.equal(inspectVersionInputs(file, text, [exception]).failures.length, 0)
 assert(inspectVersionInputs('src/features/other.tsx', text, [exception]).failures.length)
 assert(inspectVersionInputs(file, text.replace('RegisterMediaDialog', 'Launch'), [exception]).failures.length)
 assert(inspectVersionInputs(file, text.replace('media-version', 'new-version'), [exception]).failures.length)
 assert(inspectVersionInputs(file, text.replace('return <input', 'return <><input').replace('/>}', '/><input id="media-version" value={version}/></>}'), [exception]).failures.length)
})
test('the mock fixture exception is frozen, not a blanket mock page exclusion', () => {
 const exceptions = JSON.parse(readFileSync(new URL('./version-input-exceptions.json', import.meta.url)))
 const fixture = exceptions.find(e => e.kind === 'mock-protocol-fixture')
 const text = readFileSync(new URL('../src/features/ztp/ZtpOnboardBoard.tsx', import.meta.url), 'utf8')
 assert.equal(inspectVersionInputs(fixture.file, text, exceptions).failures.length, 0)
 assert(inspectVersionInputs(fixture.file, text.replace('id="kea-version"', 'id="kea-version" maxLength={120}'), exceptions).failures.length)
 assert(inspectVersionInputs(fixture.file, text.replace('id="kea-nos"', 'id="another-version"'), exceptions).failures.length)
})
test('dynamic Flow fallback refuses guard deletion, late guards, editable or non-returning branches', () => {
 const text = readFileSync(new URL('../src/features/execution/CatalogInputs.tsx', import.meta.url), 'utf8')
 assert.deepEqual(inspectDynamicCatalog(text), [])
 assert.deepEqual(inspectDynamicCatalog(text.replace("key.endsWith('.version')", 'key.endsWith(".version")')), [])
 for (const changed of [
  text.replace("key.endsWith('.version')", "key === 'profile'"),
  text.replace('if (entry) return <ParamSelect', 'if (false) return <ParamSelect'),
  text.replace("if (key.endsWith('.version')) return", "if (key === 'driver.version') return <Input value={value} />; if (key.endsWith('.version')) return"),
  text.replace('<NativeSelect id={id} disabled value="">', '<NativeSelect id={id} disabled={false} value="">'),
  text.replace("if (key.endsWith('.version')) return", "if (key.endsWith('.version')) console.log('guard removed'); return"),
 ]) assert(inspectDynamicCatalog(changed).length)
})
test('current source tree and exception inventory pass together', () => assert.deepEqual(checkTree(), []))
