import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useDesignStore } from '@pando/client/stores/designStore'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'
import { Button } from '@/components/ui'
import { Check, Download, Palette, Sparkles } from '@/components/ui/icons'

/**
 * TemplateGallery lists the design templates an artifact can be built from.
 *
 * "Try it" pushes the template's starter brief into the chat composer instead
 * of creating anything: a template is only half the input, and the other half
 * is what the user actually wants. Sending them to the composer with the brief
 * already written is the shortest honest path from a picked card to an artifact.
 */
export default function TemplateGallery() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const templates = useDesignStore((s) => s.templates)
  const craft = useDesignStore((s) => s.craftReferences)
  const loading = useDesignStore((s) => s.templatesLoading)
  const fetchTemplates = useDesignStore((s) => s.fetchTemplates)
  const installTemplate = useDesignStore((s) => s.installTemplate)
  const insertIntoDraft = useChatDraftStore((s) => s.insertIntoDraft)

  useEffect(() => {
    void fetchTemplates()
  }, [fetchTemplates])

  const tryIt = (name: string, prompt: string) => {
    insertIntoDraft(prompt ? `Use the ${name} design template. ${prompt}` : `Use the ${name} design template.`)
    navigate('/chat')
  }

  if (loading && templates.length === 0) {
    return <div className="design-loading">{t('design.templates.loading')}</div>
  }

  if (templates.length === 0) {
    return (
      <div className="design-empty">
        <Sparkles size={22} />
        {t('design.templates.empty')}
      </div>
    )
  }

  const startable = templates.filter((tpl) => tpl.startable)
  const others = templates.filter((tpl) => !tpl.startable)

  return (
    <div className="design-templates">
      <p className="design-templates-hint">{t('design.templates.hint')}</p>

      <div className="design-templates-grid">
        {startable.map((tpl) => (
          <div key={tpl.name} className="design-template-card">
            <div className="design-template-title-row">
              <span className="design-template-name">{tpl.name}</span>
              {tpl.kind && <span className="design-template-kind">{tpl.kind}</span>}
            </div>

            <div className="design-template-desc">{tpl.description}</div>

            {tpl.requires_system && (
              <div className="design-template-note">
                <Palette size={12} />
                {t('design.templates.needsSystem')}
              </div>
            )}

            {tpl.example_prompt && <div className="design-template-prompt">{tpl.example_prompt}</div>}

            <div className="design-template-actions">
              <Button size="sm" variant="primary" block onClick={() => tryIt(tpl.name, tpl.example_prompt ?? '')}>
                {t('design.templates.tryIt')}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                icon={tpl.installed ? <Check size={12} /> : <Download size={12} />}
                disabled={tpl.installed}
                onClick={() => void installTemplate(tpl.name)}
                title={tpl.installed ? tpl.source_path : t('design.templates.installHint')}
              >
                {tpl.installed ? t('design.templates.installed') : t('design.templates.install')}
              </Button>
            </div>
          </div>
        ))}
      </div>

      {others.length > 0 && (
        <div className="design-templates-workflows">
          <strong style={{ color: 'var(--fg)' }}>{t('design.templates.workflows')}</strong>
          {others.map((tpl) => (
            <div key={tpl.name}>
              <code>{tpl.name}</code> — {tpl.description}
            </div>
          ))}
        </div>
      )}

      {craft.length > 0 && <div className="design-templates-craft">{t('design.templates.craft')}: {craft.join(', ')}</div>}
    </div>
  )
}
