// CSV import for BOM (FR-2.1): the network team delivers a flat sheet where
// each row is one MAC; we parse it client-side and fold it into the standard
// BomImportInput JSON so the existing preview/commit endpoints stay the only
// authority. XLSX is not parsed here (binary format) and stays planned.

export type CsvParseResult =
  | { ok: true; input: { file_version: string; machines: unknown[] }; rows: number }
  | { ok: false; errors: string[] }

const HEADERS: Record<string, string> = {
  serial_number: 'serial_number', sn: 'serial_number', '序列号': 'serial_number',
  rack: 'rack', '机柜': 'rack',
  unit_position: 'unit_position', 'u位': 'unit_position', 'u 位': 'unit_position',
  bmc_address: 'bmc_address', bmc: 'bmc_address', 'bmc 地址': 'bmc_address',
  hostname: 'hostname', '主机名': 'hostname',
  ip_reference: 'ip_reference', ip: 'ip_reference', 'ip 参考': 'ip_reference',
  model_name: 'model_name', model: 'model_name', '型号': 'model_name',
  mac_address: 'mac_address', mac: 'mac_address', 'mac 地址': 'mac_address',
  port_name: 'port_name', port: 'port_name', '端口': 'port_name',
  rail: 'rail',
}

function splitRows(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = [], field = '', quoted = false
  for (let i = 0; i < text.length; i++) {
    const char = text[i]
    if (quoted) {
      if (char === '"') {
        if (text[i + 1] === '"') { field += '"'; i++ } else quoted = false
      } else field += char
    } else if (char === '"') quoted = true
    else if (char === ',') { row.push(field); field = '' }
    else if (char === '\n') { row.push(field); rows.push(row); row = []; field = '' }
    else if (char === '\r') { /* tolerate CRLF */ }
    else field += char
  }
  if (field || row.length) { row.push(field); rows.push(row) }
  return rows.filter((cells) => cells.some((cell) => cell.trim() !== ''))
}

export function parseBomCsv(text: string, fileVersion: string): CsvParseResult {
  const errors: string[] = []
  if (!fileVersion.trim()) errors.push('file_version is empty')
  const rows = splitRows(text)
  if (rows.length < 2) return { ok: false, errors: ['CSV needs a header row and at least one data row'] }
  const header = rows[0].map((cell) => cell.trim().toLowerCase())
  const index: Record<string, number> = {}
  header.forEach((name, position) => {
    const canonical = HEADERS[name]
    if (canonical && !(canonical in index)) index[canonical] = position
  })
  if (!('serial_number' in index)) errors.push('header must include serial_number/SN/序列号')
  if (!('mac_address' in index)) errors.push('header must include mac_address/MAC')
  if (!('hostname' in index)) errors.push('header must include hostname/主机名')
  if (errors.length) return { ok: false, errors }

  const machines = new Map<string, { machine: Record<string, unknown>; seenMacs: Set<string> }>()
  for (let r = 1; r < rows.length; r++) {
    const cells = rows[r]
    const pick = (key: string) => (index[key] !== undefined ? (cells[index[key]] ?? '').trim() : '')
    const sn = pick('serial_number')
    if (!sn) { errors.push(`row ${r + 1}: serial number is empty`); continue }
    const mac = pick('mac_address')
    if (!mac) { errors.push(`row ${r + 1} (${sn}): MAC is empty`); continue }
    let entry = machines.get(sn)
    if (!entry) {
      entry = {
        machine: {
          serial_number: sn, rack: pick('rack'), unit_position: pick('unit_position'),
          bmc_address: pick('bmc_address'), hostname: pick('hostname'),
          ip_reference: pick('ip_reference'), model_name: pick('model_name'),
          ports: [],
        },
        seenMacs: new Set<string>(),
      }
      machines.set(sn, entry)
    }
    if (entry.seenMacs.has(mac)) continue // exact duplicate row: keep one port
    entry.seenMacs.add(mac)
    const rail = Number(pick('rail'))
    ;(entry.machine.ports as unknown[]).push({
      mac_address: mac, port_name: pick('port_name') || `port${entry.seenMacs.size}`,
      rail: Number.isSafeInteger(rail) && rail >= 0 ? rail : 0,
    })
    if (!entry.machine.hostname) entry.machine.hostname = pick('hostname')
  }
  if (!machines.size) errors.push('no machines parsed')
  if (errors.length) return { ok: false, errors }
  return { ok: true, input: { file_version: fileVersion.trim(), machines: [...machines.values()].map((e) => e.machine) }, rows: rows.length - 1 }
}
