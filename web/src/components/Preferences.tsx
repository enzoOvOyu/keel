import {Button} from '@/components/ui/button'
import {Separator} from '@/components/ui/separator'
import {Tooltip,TooltipContent,TooltipTrigger} from '@/components/ui/tooltip'
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
    <Button type="button" variant="ghost" className="ui:h-[var(--size-control)] ui:px-3 ui:text-muted-foreground" onClick={toggleLanguage} aria-label={t('language')}><Icon name="globe" /><span>{i18n.language === 'en' ? '中文' : 'EN'}</span></Button>
    <Separator orientation="vertical" className="ui:h-4" />
    <Tooltip><TooltipTrigger asChild><Button type="button" variant="ghost" size="icon" className="ui:size-[var(--size-control)] ui:text-muted-foreground" onClick={toggleTheme} aria-label={t(theme === 'dark' ? 'lightTheme' : 'darkTheme')}><Icon name={theme === 'dark' ? 'sun' : 'moon'} /></Button></TooltipTrigger><TooltipContent>{t(theme === 'dark' ? 'lightTheme' : 'darkTheme')}</TooltipContent></Tooltip>
  </div>
}
