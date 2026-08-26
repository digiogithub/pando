import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faWandMagicSparkles, faDownload, faCheck, faSwatchbook } from '@fortawesome/free-solid-svg-icons'
import { useDesignStore } from '@pando/client/stores/designStore'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'

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
    return (
      <div style={{ padding: '1.5rem', color: 'var(--fg-muted)', fontSize: 13 }}>
        {t('design.templates.loading')}
      </div>
    )
  }

  if (templates.length === 0) {
    return (
      <div style={{ padding: '2rem', maxWidth: 560, color: 'var(--fg-muted)', fontSize: 13, lineHeight: 1.7 }}>
        <FontAwesomeIcon icon={faWandMagicSparkles} style={{ fontSize: 22, marginBottom: '0.75rem', display: 'block' }} />
        {t('design.templates.empty')}
      </div>
    )
  }

  const startable = templates.filter((tpl) => tpl.startable)
  const others = templates.filter((tpl) => !tpl.startable)

  return (
    <div style={{ overflow: 'auto', padding: '1rem' }}>
      <p style={{ margin: '0 0 1rem', color: 'var(--fg-muted)', fontSize: 12, maxWidth: '70ch', lineHeight: 1.6 }}>
        {t('design.templates.hint')}
      </p>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
          gap: '1rem',
        }}
      >
        {startable.map((tpl) => (
          <div
            key={tpl.name}
            style={{
              border: '1px solid var(--border)',
              borderRadius: 6,
              padding: '0.9rem',
              display: 'flex',
              flexDirection: 'column',
              gap: '0.5rem',
              background: 'var(--bg-elevated, transparent)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.5rem' }}>
              <span style={{ fontWeight: 600, fontSize: 14 }}>{tpl.name}</span>
              {tpl.kind && (
                <span style={{ fontSize: 11, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
                  {tpl.kind}
                </span>
              )}
            </div>

            <div style={{ fontSize: 12, color: 'var(--fg-muted)', lineHeight: 1.5 }}>{tpl.description}</div>

            {tpl.requires_system && (
              <div style={{ fontSize: 11, color: 'var(--fg-muted)' }}>
                <FontAwesomeIcon icon={faSwatchbook} style={{ marginRight: 6 }} />
                {t('design.templates.needsSystem')}
              </div>
            )}

            {tpl.example_prompt && (
              <div
                style={{
                  fontSize: 11,
                  fontStyle: 'italic',
                  color: 'var(--fg-muted)',
                  borderLeft: '2px solid var(--border)',
                  paddingLeft: '0.5rem',
                  lineHeight: 1.5,
                }}
              >
                {tpl.example_prompt}
              </div>
            )}

            <div style={{ display: 'flex', gap: '0.5rem', marginTop: 'auto', paddingTop: '0.4rem' }}>
              <button
                type="button"
                onClick={() => tryIt(tpl.name, tpl.example_prompt ?? '')}
                style={{
                  flex: 1,
                  padding: '0.35rem 0.6rem',
                  fontSize: 12,
                  cursor: 'pointer',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  background: 'var(--accent, transparent)',
                  color: 'var(--accent-fg, inherit)',
                }}
              >
                {t('design.templates.tryIt')}
              </button>
              <button
                type="button"
                disabled={tpl.installed}
                onClick={() => void installTemplate(tpl.name)}
                title={tpl.installed ? tpl.source_path : t('design.templates.installHint')}
                style={{
                  padding: '0.35rem 0.6rem',
                  fontSize: 12,
                  cursor: tpl.installed ? 'default' : 'pointer',
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  background: 'transparent',
                  color: 'var(--fg-muted)',
                }}
              >
                <FontAwesomeIcon icon={tpl.installed ? faCheck : faDownload} style={{ marginRight: 5 }} />
                {tpl.installed ? t('design.templates.installed') : t('design.templates.install')}
              </button>
            </div>
          </div>
        ))}
      </div>

      {others.length > 0 && (
        <div style={{ marginTop: '1.5rem', fontSize: 12, color: 'var(--fg-muted)', lineHeight: 1.7 }}>
          <strong style={{ color: 'var(--fg)' }}>{t('design.templates.workflows')}</strong>
          {others.map((tpl) => (
            <div key={tpl.name}>
              <code>{tpl.name}</code> — {tpl.description}
            </div>
          ))}
        </div>
      )}

      {craft.length > 0 && (
        <div style={{ marginTop: '1rem', fontSize: 12, color: 'var(--fg-muted)' }}>
          {t('design.templates.craft')}: {craft.join(', ')}
        </div>
      )}
    </div>
  )
}
