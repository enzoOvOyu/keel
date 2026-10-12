import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from './Icon'
import { writePreference } from '../lib/preferences'
export function Preferences() {
  const { t, i18n } = useTranslation()
  const [theme, setTheme] = useState(document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light')
  function toggleTheme() {
    const next = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.dataset.theme = next
    writePreference('keel.theme', next)
    setTheme(next)
  }
  function toggleLanguage() {
    const next = i18n.language === 'en' ? 'zh' : 'en'
    writePreference('keel.language', next)
    void i18n.changeLanguage(next)
  }
  return <div className="preferences">
    <button type="button" className="language-button" onClick={toggleLanguage} aria-label={t('language')}><Icon name="globe" /><span>{i18n.language === 'en' ? '中文' : 'EN'}</span></button>
    <span className="preference-divider" aria-hidden="true" />
    <button type="button" className="icon-button" onClick={toggleTheme} aria-label={t(theme === 'dark' ? 'lightTheme' : 'darkTheme')}><Icon name={theme === 'dark' ? 'sun' : 'moon'} /></button>
  </div>
}
