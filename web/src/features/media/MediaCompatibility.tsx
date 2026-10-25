import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { ApiError } from '../../lib/api'
import { ActionError } from '../../components/Management'
import { validateMediaCompatibility, type MediaCompatibility } from '../../lib/media'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export function MediaCompatibilitySummary({ matrix }: { matrix?: MediaCompatibility }) {
 const { t } = useTranslation()
 if (!matrix) return null
 const columns = Object.keys(matrix.combinations?.[0] ?? {}).sort()
 return <section aria-label={t('mediaCompatibility')} className="ui:space-y-3 ui:my-4">
  <h3>{t('mediaCompatibility')}</h3>
  <dl className="action-target"><dt>{t('mediaMatrixSource')}</dt><dd className="ui:break-all">{matrix.source}</dd><dt>{t('mediaMatrixVersion')}</dt><dd className="ui:break-all">{matrix.version}</dd><dt>{t('mediaMatrixReport')}</dt><dd className="ui:break-all">{matrix.report_ref}</dd></dl>
  {Object.keys(matrix.requires ?? {}).sort().map(key => <p key={key} className="ui:break-all"><strong>{key}</strong>: {matrix.requires![key].join(' · ')}</p>)}
  {!!columns.length && <div role="region" tabIndex={0} aria-label={t('mediaMatrixRows')} className="ui:max-h-72 ui:overflow-auto"><Table><TableHeader><TableRow>{columns.map(key => <TableHead scope="col" key={key}>{key}</TableHead>)}</TableRow></TableHeader><TableBody>{matrix.combinations!.map((row, index) => <TableRow key={index}>{columns.map(key => <TableCell key={key}>{row[key]}</TableCell>)}</TableRow>)}</TableBody></Table></div>}
  <p className="field-hint">{t('mediaMatrixTrust')}</p>
 </section>
}

export function MediaCompatibilityImport({ projectId, disabled, value, onChange, onBlocked }: {
 projectId: string | null; disabled: boolean; value?: MediaCompatibility;
 onChange: (value: MediaCompatibility | undefined) => void; onBlocked: (blocked: boolean) => void
}) {
 const { t } = useTranslation(), session = useSession(), serial = useRef(0), controller = useRef<AbortController | null>(null), input = useRef<HTMLInputElement>(null)
 const [status, setStatus] = useState<'empty' | 'validating' | 'invalid' | 'valid'>('empty'), [error, setError] = useState<unknown>(null)
 useExpireOnUnauthorized(error)
 useEffect(() => () => { serial.current++; controller.current?.abort() }, [])
 function clear() {
  serial.current++; controller.current?.abort(); controller.current = null
  if (input.current) input.current.value = ''
  setStatus('empty'); setError(null); onChange(undefined); onBlocked(false)
 }
 async function load(file: File | undefined) {
  if (!file) { clear(); return }
  const attempt = ++serial.current
  controller.current?.abort(); const abort = new AbortController(); controller.current = abort
  setStatus('validating'); setError(null); onChange(undefined); onBlocked(true)
  try {
   if (file.size > 65536) throw new Error('file limit')
   const raw: unknown = JSON.parse(await file.text())
   if (serial.current !== attempt) return
   const matrix = await validateMediaCompatibility(projectId, raw, session.csrf_token, abort.signal)
   if (serial.current !== attempt) return
   onChange(matrix); onBlocked(false); setStatus('valid')
  } catch (cause) {
   if (serial.current !== attempt || abort.signal.aborted) return
   setError(cause); setStatus('invalid')
  }
 }
 return <div className="field">
  <Label htmlFor="media-matrix-file">{t('mediaMatrixImport')}</Label>
  <Input ref={input} id="media-matrix-file" type="file" accept=".json,application/json" disabled={disabled} onChange={event => void load(event.target.files?.[0])} aria-describedby="media-matrix-hint" />
  <p id="media-matrix-hint" className="field-hint">{t('mediaMatrixImportHint')}</p>
  {status === 'validating' && <p role="status">{t('mediaMatrixValidating')}</p>}
  {status === 'invalid' && <><p role="alert" className="field-error">{t('mediaMatrixInvalid')}</p>{error instanceof ApiError && <ActionError error={error} />}</>}
  {status !== 'empty' && <Button type="button" variant="outline" disabled={disabled} onClick={clear}>{t('mediaMatrixRemove')}</Button>}
  <MediaCompatibilitySummary matrix={value} />
 </div>
}
