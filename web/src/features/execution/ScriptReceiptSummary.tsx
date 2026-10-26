import { useTranslation } from 'react-i18next'

function object(value:unknown): Record<string,unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string,unknown> : undefined
}

export function ScriptReceiptSummary({ output }: { output:unknown }) {
  const { t } = useTranslation()
  const outer = object(output), observed = object(outer?.observation)
  if (!outer || outer.source !== 'host' || outer.operation !== 'script.run' || !['ssh','agent'].includes(String(outer.transport)) || !observed) return null
  const started = observed.process_started, stopped = observed.process_tree_stopped
  const reason = typeof observed.reason === 'string' ? observed.reason : ''
  const reasons: Record<string,string> = {
    script_completed: 'scriptReceiptCompleted',
    script_nonzero_exit: 'scriptReceiptNonzero',
    script_output_limit: 'scriptReceiptOutputLimit',
    script_output_incomplete: 'scriptReceiptOutputIncomplete',
    script_input_incomplete: 'scriptReceiptInputIncomplete',
    script_cache_rejected: 'scriptReceiptCacheRejected',
    script_json_invalid: 'scriptReceiptJSONInvalid',
    script_authorization_expired_before_start: 'scriptReceiptExpired',
    script_deadline: 'scriptReceiptDeadline',
    script_cancelled: 'scriptReceiptCancelled',
    script_interpreter_unavailable: 'scriptReceiptInterpreterUnavailable',
    script_workspace_unavailable: 'scriptReceiptWorkspaceUnavailable',
    script_process_not_started: 'scriptReceiptNotStarted',
  }
  return <section className="impact-notice" aria-label={t('scriptReceiptTitle')}>
    <h4>{t('scriptReceiptTitle')}</h4>
    <p>{t('scriptReceiptBoundary', { transport: String(outer.transport).toUpperCase() })}</p>
    <dl className="action-target">
      <dt>{t('scriptReceiptProcess')}</dt><dd>{t(started === true ? 'scriptReceiptStarted' : started === false ? 'scriptReceiptNotStarted' : 'scriptReceiptUnknown')}</dd>
      <dt>{t('scriptReceiptStop')}</dt><dd>{t(stopped === true ? started === false ? 'scriptReceiptNoProcessToStop' : 'scriptReceiptStopped' : 'scriptReceiptStopUnknown')}</dd>
      {typeof observed.cache_hit === 'boolean' && <><dt>{t('scriptReceiptCache')}</dt><dd>{t(observed.cache_hit ? 'scriptReceiptCacheHit' : 'scriptReceiptCacheMiss')}</dd></>}
      {started === true && typeof observed.exit_code === 'number' && Number.isInteger(observed.exit_code) && <><dt>{t('scriptReceiptExit')}</dt><dd>{observed.exit_code}</dd></>}
      {reason && <><dt>{t('scriptReceiptReason')}</dt><dd>{t(reasons[reason] ?? 'scriptReceiptOtherReason')}</dd></>}
    </dl>
    {observed.nonstart_proof === true && started === false && stopped === true && <p>{t('scriptReceiptNonstartProof')}</p>}
    {typeof observed.script_sha256 === 'string' && /^[a-f0-9]{64}$/.test(observed.script_sha256) && <code className="ui:break-all">{observed.script_sha256}</code>}
  </section>
}
