import { expect, test } from '@playwright/test'

const baseURL = process.env.PANDO_E2E_BASE_URL

test.describe('project tabs', () => {
  test.skip(!baseURL, 'PANDO_E2E_BASE_URL not set')

  test.beforeEach(async ({ page }) => {
    const token = process.env.PANDO_E2E_TOKEN
    if (token) {
      await page.addInitScript((value) => localStorage.setItem('pando_token', value), token)
    }

    await page.addInitScript(() => {
      localStorage.setItem(
        'pando_project_tabs',
        JSON.stringify({
          order: ['proj-1', 'proj-2'],
          activeTabId: 'main',
          lastMainRoute: '/',
        }),
      )
    })
  })

  test('switches between tabs, closes with stop, and hides after the last tab closes', async ({ page }) => {
    await page.route('**/api/v1/projects/web', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          instances: [
            {
              project_id: 'proj-1',
              name: 'Project One',
              path: '/workspace/project-one',
              state: 'running',
              started_at: '2026-10-01T20:00:00Z',
              delegations: 0,
            },
            {
              project_id: 'proj-2',
              name: 'Project Two',
              path: '/workspace/project-two',
              state: 'running',
              started_at: '2026-10-01T20:01:00Z',
              delegations: 1,
            },
          ],
        }),
      })
    })

    await page.route('**/api/v1/project', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          cwd: '/workspace/main-app',
          version: '1.0.0',
        }),
      })
    })

    await page.route('**/api/v1/projects/*/web/close', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'closed',
          cancelled_delegations: 0,
        }),
      })
    })

    await page.goto('/')
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toBeVisible()
    await expect(page.getByRole('tab', { name: /main-app/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /Project One/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /Project Two/i })).toBeVisible()

    await page.getByRole('tab', { name: /Project One/i }).click()
    await expect(page.getByRole('tab', { name: /Project One/i })).toHaveAttribute('aria-selected', 'true')

    await page.getByRole('button', { name: 'Close current project tab' }).first().click()
    await page.getByRole('button', { name: 'Close and stop workspace' }).click()
    await expect(page.getByRole('tab', { name: /Project One/i })).toHaveCount(0)

    await page.getByRole('button', { name: 'Close current project tab' }).click()
    await expect(page.getByRole('tablist', { name: 'Project tabs' })).toHaveCount(0)
  })

  test('hosts the child workspace frame for chat and terminal interactions', async ({ page }) => {
    await page.route('**/api/v1/projects/web', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          instances: [
            {
              project_id: 'proj-1',
              name: 'Project One',
              path: '/workspace/project-one',
              state: 'running',
              started_at: '2026-10-01T20:00:00Z',
            },
          ],
        }),
      })
    })

    await page.route('**/api/v1/project', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          cwd: '/workspace/main-app',
          version: '1.0.0',
        }),
      })
    })

    await page.route('**/api/v1/projects/proj-1/web/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: `<!doctype html>
<html lang="en">
  <body style="margin:0;font-family:system-ui;background:#111827;color:#f9fafb">
    <div style="display:flex;height:100vh">
      <aside style="width:180px;padding:16px;border-right:1px solid #374151">
        <button id="nav-chat" type="button">Chat</button>
        <button id="nav-terminal" type="button">Terminal</button>
      </aside>
      <main style="flex:1;padding:16px">
        <section id="chat-view">
          <label for="chat-input">Chat input</label>
          <textarea id="chat-input" aria-label="Chat input"></textarea>
          <button id="send" type="button">Send</button>
          <div id="reply" aria-label="Reply area"></div>
        </section>
        <section id="terminal-view" hidden>
          <h1>Terminal</h1>
          <div aria-label="Terminal output">project terminal ready</div>
        </section>
      </main>
    </div>
    <script>
      const chatInput = document.getElementById('chat-input');
      const reply = document.getElementById('reply');
      const chatView = document.getElementById('chat-view');
      const terminalView = document.getElementById('terminal-view');
      const post = (message) => window.parent.postMessage(message, window.location.origin);

      post({ type: 'pando:title', title: 'Frame chat' });

      document.getElementById('send').addEventListener('click', () => {
        post({ type: 'pando:busy', busy: true });
        reply.textContent = 'Echo: ' + chatInput.value;
        post({ type: 'pando:busy', busy: false });
      });

      document.getElementById('nav-chat').addEventListener('click', () => {
        chatView.hidden = false;
        terminalView.hidden = true;
        post({ type: 'pando:title', title: 'Frame chat' });
      });

      document.getElementById('nav-terminal').addEventListener('click', () => {
        chatView.hidden = true;
        terminalView.hidden = false;
        post({ type: 'pando:title', title: 'Terminal' });
      });

      window.addEventListener('message', (event) => {
        if (event.origin !== window.location.origin) return;
        if (event.data && event.data.type === 'pando:focus') {
          chatInput.focus();
        }
      });
    </script>
  </body>
</html>`,
      })
    })

    await page.goto('/')
    await page.getByRole('tab', { name: /Project One/i }).click()

    const frame = page.frameLocator('iframe[title="Project One"]')
    await frame.getByLabel('Chat input').fill('hello from the project frame')
    await frame.getByRole('button', { name: 'Send' }).click()
    await expect(frame.getByLabel('Reply area')).toContainText('Echo: hello from the project frame')

    await frame.getByRole('button', { name: 'Terminal' }).click()
    await expect(frame.getByRole('heading', { name: 'Terminal' })).toBeVisible()
    await expect(frame.getByLabel('Terminal output')).toContainText('project terminal ready')
  })
})
