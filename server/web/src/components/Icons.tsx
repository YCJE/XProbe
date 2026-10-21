import type { SVGProps } from 'react'

type Props = SVGProps<SVGSVGElement> & { size?: number }

function Svg({ size = 18, children, ...rest }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth={1.8} strokeLinecap="round"
      strokeLinejoin="round" aria-hidden="true" {...rest}>
      {children}
    </svg>
  )
}

export const IconLogo = (p: Props) => (
  <Svg {...p}>
    <path d="M4 15.5 9.5 6h2.2L6.2 19H4z" />
    <path d="M10.5 19 16 9.5h2.2L12.7 19z" />
    <circle cx="18.5" cy="5.5" r="1.6" fill="currentColor" stroke="none" />
  </Svg>
)
export const IconDashboard = (p: Props) => <Svg {...p}><rect x="3" y="3" width="7" height="8" rx="2"/><rect x="14" y="3" width="7" height="5" rx="2"/><rect x="14" y="12" width="7" height="9" rx="2"/><rect x="3" y="15" width="7" height="6" rx="2"/></Svg>
export const IconServer = (p: Props) => <Svg {...p}><rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01M11 7.5h2M11 16.5h2"/></Svg>
export const IconSettings = (p: Props) => <Svg {...p}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.56V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1.11-1.56 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.56-1H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.65 8.9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.54a1.7 1.7 0 0 0 1-1.56V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.36 9c.14.6.64 1.05 1.25 1.05H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.51 1z"/></Svg>
export const IconSun = (p: Props) => <Svg {...p}><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></Svg>
export const IconMoon = (p: Props) => <Svg {...p}><path d="M21 12.8A8.5 8.5 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></Svg>
export const IconMonitor = (p: Props) => <Svg {...p}><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></Svg>
export const IconSearch = (p: Props) => <Svg {...p}><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></Svg>
export const IconPlus = (p: Props) => <Svg {...p}><path d="M12 5v14M5 12h14"/></Svg>
export const IconLogout = (p: Props) => <Svg {...p}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5M21 12H9"/></Svg>
export const IconChevronLeft = (p: Props) => <Svg {...p}><path d="m15 18-6-6 6-6"/></Svg>
export const IconActivity = (p: Props) => <Svg {...p}><path d="M3 12h4l3 8 4-16 3 8h4"/></Svg>
export const IconWifi = (p: Props) => <Svg {...p}><path d="M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0M12 19.5h.01"/></Svg>
export const IconArrowDown = (p: Props) => <Svg {...p}><path d="M12 5v14M6 13l6 6 6-6"/></Svg>
export const IconArrowUp = (p: Props) => <Svg {...p}><path d="M12 19V5M6 11l6-6 6 6"/></Svg>
export const IconAlert = (p: Props) => <Svg {...p}><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4M12 17h.01"/></Svg>
export const IconCopy = (p: Props) => <Svg {...p}><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></Svg>
export const IconX = (p: Props) => <Svg {...p}><path d="M18 6 6 18M6 6l12 12"/></Svg>
export const IconCheck = (p: Props) => <Svg {...p}><path d="m5 12 4 4L19 6"/></Svg>
export const IconCpu = (p: Props) => <Svg {...p}><rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v2M15 2v2M9 20v2M15 20v2M2 9h2M2 15h2M20 9h2M20 15h2"/></Svg>
export const IconMemory = (p: Props) => <Svg {...p}><rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 7V4M12 7V4M17 7V4M7 17v3M12 17v3M17 17v3"/></Svg>
export const IconDisk = (p: Props) => <Svg {...p}><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2"/><path d="M12 3v4M18.4 7.6l-2.8 2.8"/></Svg>
export const IconRefresh = (p: Props) => <Svg {...p}><path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6"/></Svg>
export const IconShield = (p: Props) => <Svg {...p}><path d="M12 2 4 5v6c0 5 3.4 9.4 8 11 4.6-1.6 8-6 8-11V5z"/><path d="m9 12 2 2 4-4"/></Svg>
export const IconBell = (p: Props) => <Svg {...p}><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></Svg>
export const IconGlobe = (p: Props) => <Svg {...p}><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></Svg>
