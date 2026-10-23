import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import { readPreference } from './lib/preferences'
import './lib/i18n'
import './styles/tokens.css'
import './styles/app.css'
import './styles/management.css'
import './styles/workspace.css'

const storedTheme = readPreference('keel.theme')
document.documentElement.dataset.theme = storedTheme === 'dark' || storedTheme === 'light'
  ? storedTheme : window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } })
const router = createRouter({ routeTree, defaultPreload: 'intent', scrollRestoration: true })
declare module '@tanstack/react-router' { interface Register { router: typeof router } }

const mount = document.getElementById('root')
if (!mount) throw new Error('Missing application root')
createRoot(mount).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>
  </StrictMode>,
)
