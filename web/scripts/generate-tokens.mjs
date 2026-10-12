import { readFile, writeFile } from 'node:fs/promises'
const source = new URL('../src/styles/tokens.json', import.meta.url)
const output = new URL('../src/styles/tokens.css', import.meta.url)
const { light, darkOverrides } = JSON.parse(await readFile(source, 'utf8'))
for (const key of Object.keys(darkOverrides)) if (!(key in light)) throw new Error(`Unknown dark token: ${key}`)
const dark = { ...light, ...darkOverrides }
if (Object.keys(light).sort().join() !== Object.keys(dark).sort().join()) throw new Error('Theme key mismatch')
function rule(selector, values) {
  return `${selector} {\n${Object.entries(values).map(([key, value]) => {
    if (!/^[a-z][a-z0-9-]*$/.test(key) || typeof value !== 'string' || /[{};]/.test(value)) throw new Error(`Invalid token: ${key}`)
    return `  --${key}: ${value};`
  }).join('\n')}\n}`
}
await writeFile(output, `/* Generated from tokens.json. Do not edit by hand. */\n${rule(':root', light)}\n${rule(':root[data-theme="dark"]', dark)}\n`)
console.log(`Generated ${Object.keys(light).length} tokens for light and dark themes`)
