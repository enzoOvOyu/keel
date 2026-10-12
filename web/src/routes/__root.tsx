import { createRootRoute, Link, Outlet } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: Outlet,
  notFoundComponent: () => <main className="fallback"><h1>404</h1><p>页面不存在 / Page not found</p><Link to="/login">返回 keel / Return to keel</Link></main>,
  errorComponent: () => <main className="fallback"><h1>keel</h1><p>页面加载失败，请刷新重试。 / Unable to load this page.</p><a href="/login">重新加载 / Reload</a></main>,
})
