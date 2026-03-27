import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSettingsStore } from '@/stores/settingsStore'

/**
 * Syncs the app language with the `language` field in settings config.
 * Must be called once at the app root level.
 */
export function useLanguageSync() {
  const { i18n } = useTranslation()
  const language = useSettingsStore((s) => s.config.language)

  useEffect(() => {
    if (language && language !== i18n.language) {
      i18n.changeLanguage(language)
    }
  }, [language, i18n])
}
