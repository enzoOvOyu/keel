export type IconName = 'eye' | 'eyeOff' | 'moon' | 'sun' | 'arrow' | 'shield' | 'refresh' | 'globe' | 'external' | 'menu' | 'folder' | 'overview' | 'box' | 'network' | 'play' | 'layers' | 'archive' | 'users' | 'audit'
const paths: Record<IconName, string> = {
  eye: 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Zm13 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z',
  eyeOff: 'm3 3 18 18M10.6 5.1C17.5 4 22 12 22 12a21 21 0 0 1-3 3.9M6.2 6.2A24 24 0 0 0 2 12s3.5 7 10 7a12 12 0 0 0 5.8-1.8M9.9 9.9a3 3 0 0 0 4.2 4.2',
  moon: 'M21 13.2A9 9 0 1 1 10.8 3 7 7 0 0 0 21 13.2Z',
  sun: 'M12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  arrow: 'M5 12h14m-5-5 5 5-5 5',
  shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Zm-3 9 2 2 4-4',
  refresh: 'M20 8a8 8 0 1 0 .2 7M20 3v5h-5',
  globe: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM3 12h18M12 3c-4 4-4 14 0 18 4-4 4-14 0-18Z',
  menu: 'M4 6h16M4 12h16M4 18h16',
  folder: 'M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z',
  overview: 'M3 3h7v7H3V3Zm11 0h7v7h-7V3ZM3 14h7v7H3v-7Zm11 0h7v7h-7v-7Z',
  box: 'm12 3 9 5v8l-9 5-9-5V8l9-5Zm-9 5 9 5 9-5M12 13v8M7.5 5.5l9 5',
  network: 'M9 3h6v6H9V3ZM3 15h6v6H3v-6Zm12 0h6v6h-6v-6ZM12 9v3M6 15v-3h12v3',
  play: 'm8 4 12 8-12 8V4Z',
  layers: 'm12 3 10 5-10 5L2 8l10-5ZM2 12l10 5 10-5M2 16l10 5 10-5',
  archive: 'M3 3h18v5H3V3Zm2 5v13h14V8M9 12h6',
  users: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M16 4a4 4 0 0 1 0 8M22 21v-2a4 4 0 0 0-3-3.9M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  audit: 'M8 3H5v18h14V3h-3M8 2h8v4H8V2Zm0 9h8M8 15h8',
  external: 'M14 3h7v7m0-7-10 10M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5',
}
export function Icon({ name, className = '' }: { name: IconName; className?: string }) {
  return <svg className={`icon ${className}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d={paths[name]} /></svg>
}
