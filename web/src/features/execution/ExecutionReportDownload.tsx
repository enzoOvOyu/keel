import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { ActionError } from '../../components/Management'
import { ApiError } from '../../lib/api'
import { getExecutionReport } from '../../lib/execution-views'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized } from '../../lib/session'

export function ExecutionReportDownload({ projectId, executionId, onDenied }: { projectId:string; executionId:string; onDenied:(error:ApiError)=>void }) {
  const { t } = useTranslation(), action = useAction(), request = useRef<AbortController | null>(null)
  useExpireOnUnauthorized(action.error)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => { if (action.error instanceof ApiError && [401,403,404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  async function download() {
    const controller = new AbortController()
    request.current?.abort(); request.current = controller
    const result = await action.run(() => getExecutionReport(projectId, executionId, controller.signal))
    if (!result.ok || controller.signal.aborted) return
    const url = URL.createObjectURL(new Blob([JSON.stringify(result.value,null,2)+'\n'],{type:'application/json;charset=utf-8'}))
    const link = document.createElement('a')
    link.href = url; link.download = `keel-execution-${executionId}-report.json`
    document.body.append(link); link.click(); link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url),0)
  }
  return <div><Button type="button" variant="outline" disabled={action.busy} onClick={() => void download()}>{t('executionReportDownload')}</Button><p className="field-hint">{t('executionReportBoundary')}</p><ActionError error={action.error} /></div>
}
