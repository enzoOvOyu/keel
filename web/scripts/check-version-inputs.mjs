/** FR-1.9 / DS §9: reject recognizable execution version controls outside ParamSelect.
 * This is a source guard, not a proof of runtime admission. The API remains authoritative.
 */
import ts from 'typescript'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { createHash } from 'node:crypto'

const root = fileURLToPath(new URL('../', import.meta.url))
const versionName = /version|版本/i
const selectTags = new Set(['select', 'NativeSelect'])
const controls = new Set(['input', 'textarea', 'select', 'Input', 'Textarea', 'NativeSelect'])
const hash = text => createHash('sha256').update(text.replace(/\s+/g, ' ')).digest('hex')
function attribute(node, name) { return node.attributes.properties.find(p => ts.isJsxAttribute(p) && p.name.getText() === name) }
function literalAttribute(node, name) {
 const value = attribute(node, name)?.initializer
 return value && ts.isStringLiteral(value) ? value.text : undefined
}
function booleanTrue(node, name) {
 const attr = attribute(node, name)
 return !!attr && (!attr.initializer || ts.isJsxExpression(attr.initializer) && attr.initializer.expression?.kind === ts.SyntaxKind.TrueKeyword)
}
function stableAttribute(node, name) {
 const index = node.attributes.properties.findIndex(p => ts.isJsxAttribute(p) && p.name.getText() === name)
 return index >= 0 && !node.attributes.properties.slice(index + 1).some(p => ts.isJsxSpreadAttribute(p) || ts.isJsxAttribute(p) && p.name.getText() === name)
}
function versionCondition(expr) {
 return ts.isCallExpression(expr) && ts.isPropertyAccessExpression(expr.expression) && ts.isIdentifier(expr.expression.expression) && expr.expression.expression.text === 'key' && expr.expression.name.text === 'endsWith' && expr.arguments.length === 1 && ts.isStringLiteral(expr.arguments[0]) && expr.arguments[0].text === '.version'
}
function component(node) {
 for (let parent = node.parent; parent; parent = parent.parent) {
  if (ts.isFunctionDeclaration(parent)) return parent.name?.text
  if (ts.isVariableDeclaration(parent) && parent.initializer && (ts.isArrowFunction(parent.initializer) || ts.isFunctionExpression(parent.initializer))) return parent.name.getText()
 }
}
function labelText(node) {
 if (ts.isJsxElement(node)) {
  if (controls.has(node.openingElement.tagName.getText())) return ''
  return node.children.map(labelText).join(' ')
 }
 if (ts.isJsxSelfClosingElement(node)) return ''
 return node.getText()
}
function inputLabel(node) {
 // Include an adjacent/wrapping label: value={target} alone must not conceal a version field.
 let parent = node.parent
 if (ts.isJsxElement(parent) && parent.openingElement === node) parent = parent.parent
 if (!parent || !ts.isJsxElement(parent)) return ''
 if (['label', 'Label'].includes(parent.openingElement.tagName.getText())) return labelText(parent)
 return parent.children.filter(child => ts.isJsxElement(child) && ['label', 'Label'].includes(child.openingElement.tagName.getText())).map(labelText).join(' ')
}
export function inspectVersionInputs(file, text, exceptions = []) {
 const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
 const aliases = new Map(), failures = source.parseDiagnostics.map(d => `${file}: ${ts.flattenDiagnosticMessageText(d.messageText, ' ')}`), used = new Set()
 for (const stmt of source.statements) {
  if (!ts.isImportDeclaration(stmt) || !ts.isStringLiteral(stmt.moduleSpecifier)) continue
  if (!/(?:^|\/)components\/ui\/(?:input|textarea|native-select)$/.test(stmt.moduleSpecifier.text)) continue
  const bindings = stmt.importClause?.namedBindings
  if (bindings && ts.isNamedImports(bindings)) for (const spec of bindings.elements) aliases.set(spec.name.text, spec.propertyName?.text ?? spec.name.text)
  if (bindings && ts.isNamespaceImport(bindings)) for (const tag of ['Input', 'Textarea', 'NativeSelect']) aliases.set(bindings.name.text + '.' + tag, tag)
 }
 function report(node, message) {
  const { line, character } = source.getLineAndCharacterOfPosition(node.getStart())
  failures.push(`${file}:${line + 1}:${character + 1}: ${message}`)
 }
 function visit(node) {
  if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
   const tag = aliases.get(node.tagName.getText()) ?? node.tagName.getText()
   if (controls.has(tag)) {
    const names = selectTags.has(tag) ? ['id', 'name', 'aria-label'] : ['id', 'name', 'value', 'defaultValue', 'onChange', 'aria-label', 'placeholder']
    const attrs = node.attributes.properties.filter(p => ts.isJsxSpreadAttribute(p) || ts.isJsxAttribute(p) && names.includes(p.name.getText())).map(p => p.getText()).join(' ')
    if (versionName.test(attrs + inputLabel(node)) && !(literalAttribute(node, 'type') === 'hidden' && stableAttribute(node, 'type')) && !(!selectTags.has(tag) && booleanTrue(node, 'readOnly') && stableAttribute(node, 'readOnly'))) {
     const exception = exceptions.find(e => e.file === file && e.component === component(node) && e.id === literalAttribute(node, 'id'))
     if (exception && used.has(exception.id)) report(node, 'Version input exception cannot be duplicated.')
     else if (exception?.kind === 'media-registration' && (tag === 'input' || tag === 'Input')) used.add(exception.id)
     else if (exception?.kind === 'mock-protocol-fixture' && exception.digest === hash(node.getText())) used.add(exception.id)
     else report(node, 'Version selection must use ParamSelect; new versions belong in media registration.')
    }
   }
  }
  ts.forEachChild(node, visit)
 }
 visit(source)
 return { failures, used }
}

export function inspectDynamicCatalog(text) {
 const file = 'src/features/execution/CatalogInputs.tsx'
 const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
 let safe = false
 function visit(node) {
  // Guard the existing dynamic Flow input renderer: the unknown-version branch must
  // unconditionally return a disabled select before any free-text fallback.
  if (ts.isBlock(node) && node.statements.some(stmt => ts.isVariableStatement(stmt) && stmt.declarationList.declarations.some(d => d.name.getText() === 'entry'))) {
   const statements = [...node.statements]
   const guard = statements.findIndex(stmt => ts.isIfStatement(stmt) && versionCondition(stmt.expression) && ts.isReturnStatement(stmt.thenStatement))
   const fallback = statements.findIndex(stmt => ts.isReturnStatement(stmt) && /<Input\b/.test(stmt.getText()))
   if (guard >= 0 && fallback > guard) {
    const result = statements[guard].thenStatement.expression
    let disabledSelect = false, editable = false
    function scan(n) {
     if (ts.isJsxOpeningElement(n) || ts.isJsxSelfClosingElement(n)) {
      if (n.tagName.getText() === 'NativeSelect' && booleanTrue(n, 'disabled') && stableAttribute(n, 'disabled')) disabledSelect = true
      if (selectTags.has(n.tagName.getText()) && (!booleanTrue(n, 'disabled') || !stableAttribute(n, 'disabled'))) editable = true
      if (['Input', 'input', 'Textarea', 'textarea'].includes(n.tagName.getText()) || attribute(n, 'onChange')) editable = true
     }
     ts.forEachChild(n, scan)
    }
    if (result) scan(result)
    safe = disabledSelect && !editable && !statements.slice(0, guard).some(stmt => /<(?:Input|input|Textarea|textarea)\b/.test(stmt.getText())) && statements.slice(0, guard).some(stmt => ts.isIfStatement(stmt) && ts.isIdentifier(stmt.expression) && stmt.expression.text === 'entry' && ts.isReturnStatement(stmt.thenStatement) && stmt.thenStatement.expression && ts.isJsxSelfClosingElement(stmt.thenStatement.expression) && stmt.thenStatement.expression.tagName.getText() === 'ParamSelect')
   }
  }
  ts.forEachChild(node, visit)
 }
 visit(source)
 return safe ? [] : [`${file}: unknown .version inputs must return a disabled select before the text fallback; registered entries must use ParamSelect.`]
}
function files(dir) { return readdirSync(dir, { withFileTypes: true }).flatMap(e => e.isDirectory() ? files(path.join(dir, e.name)) : /\.tsx$/.test(e.name) ? [path.join(dir, e.name)] : []) }
export function checkTree() {
 const exceptions = JSON.parse(readFileSync(path.join(root, 'scripts/version-input-exceptions.json'), 'utf8'))
 const failures = [], used = new Set()
 for (const absolute of files(path.join(root, 'src'))) {
  const file = path.relative(root, absolute).replaceAll(path.sep, '/')
  if (file.startsWith('src/components/ui/')) continue
  const text = readFileSync(absolute, 'utf8'), result = inspectVersionInputs(file, text, exceptions)
  failures.push(...result.failures)
  for (const id of result.used) used.add(file + ':' + id)
  if (file === 'src/features/execution/CatalogInputs.tsx') { used.add('dynamic-renderer'); failures.push(...inspectDynamicCatalog(text)) }
 }
 if (!used.has('dynamic-renderer')) failures.push('Missing guarded dynamic CatalogInputs renderer; update this source guard when moving the renderer.')
 for (const e of exceptions) {
  if (!e.reason || !['media-registration', 'mock-protocol-fixture'].includes(e.kind)) failures.push(`Invalid version input exception: ${e.id}`)
  if (!used.has(e.file + ':' + e.id)) failures.push(`Stale or changed version input exception: ${e.file}:${e.id}`)
 }
 return failures
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 const failures = checkTree()
 if (failures.length) { console.error(failures.join('\n')); process.exitCode = 1 }
 else console.log('PASS version inputs: recognized version controls use ParamSelect; media registration and one frozen mock protocol fixture are explicit exceptions; dynamic Flow fallback is guarded.')
}
