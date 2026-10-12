import { useId } from 'react'
import { useTranslation } from 'react-i18next'

export function KeelMark({ className = '' }: { className?: string }) {
  const gradient = useId()
  return <svg className={className} viewBox="0 0 48 48" fill="none" aria-hidden="true" focusable="false">
    <defs><linearGradient id={gradient} x1="8" y1="4" x2="43" y2="42" gradientUnits="userSpaceOnUse">
      <stop stopColor="var(--color-primary-500)" /><stop offset="1" stopColor="var(--color-accent-pink)" />
    </linearGradient></defs>
    <path d="M9 8v30l8 5V25L37 7H25L17 15V8H9Z" fill={`url(#${gradient})`} />
    <path d="m22 25 15 16H25l-9-10 6-6Z" fill={`url(#${gradient})`} />
  </svg>
}
export function Brand() {
  const { t } = useTranslation()
  return <div className="brand" aria-label={`keel ${t('product')}`}>
    <KeelMark className="brand-mark" />
    <div aria-hidden="true"><span className="wordmark">keel</span><span className="brand-caption">{t('product')}</span></div>
  </div>
}
