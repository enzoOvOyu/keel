import { useTranslation } from 'react-i18next'
function object(value:unknown):Record<string,unknown>|undefined{return value!==null && typeof value==='object' && !Array.isArray(value)?value as Record<string,unknown>:undefined}
export function ArtifactReceiptSummary({output}:{output:unknown}){
 const {t}=useTranslation(),outer=object(output),observed=object(outer?.observation)
 if(!outer || outer.source!=='host' || outer.operation!=='media.stage' || !['ssh','agent'].includes(String(outer.transport)) || !observed)return null
 const offset=observed.checkpoint_offset,size=observed.artifact_size
 const valid=typeof offset==='number' && Number.isSafeInteger(offset) && offset>=0 && typeof size==='number' && Number.isSafeInteger(size) && size>0 && offset<=size
 const reason=typeof observed.reason==='string'?observed.reason:''
 const reasons:Record<string,string>={artifact_verified:'artifactReceiptDone',artifact_checkpointed:'artifactReceiptPaused',artifact_cache_unavailable:'artifactReceiptUnavailable',artifact_transfer_busy:'artifactReceiptBusy',artifact_cache_rejected:'artifactReceiptRejected',artifact_digest_mismatch:'artifactReceiptMismatch'}
 return <section className="impact-notice" aria-label={t('artifactReceiptTitle')}><h4>{t('artifactReceiptTitle')}</h4><p>{t('artifactReceiptBoundary',{transport:String(outer.transport).toUpperCase()})}</p><dl className="action-target"><dt>{t('artifactReceiptCheckpoint')}</dt><dd>{valid?`${offset} / ${size}`:'—'}</dd><dt>{t('artifactReceiptVerified')}</dt><dd>{t(observed.content_verified===true && valid && offset===size?'artifactReceiptYes':'artifactReceiptNo')}</dd><dt>{t('artifactReceiptStop')}</dt><dd>{t(observed.writer_stopped===true?'artifactReceiptStopped':'artifactReceiptUnknown')}</dd><dt>{t('artifactReceiptReason')}</dt><dd>{t(reasons[reason] ?? 'artifactReceiptOther')}</dd></dl>{typeof observed.artifact_sha256==='string' && /^[a-f0-9]{64}$/.test(observed.artifact_sha256) && <code className="ui:break-all">{observed.artifact_sha256}</code>}</section>
}
