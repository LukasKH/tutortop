import type { ReactNode } from "react";

// The few drawn icons the app needs, on a 16px grid with one stroke weight.
// Everything else is text: ✓ ✗ – ← →.

type P = { size?: number; className?: string };

function Svg({ size = 16, className, children }: P & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const ChevronDown = (p: P) => (
  <Svg {...p}>
    <path d="M4 6l4 4 4-4" />
  </Svg>
);

export const PanelLeft = (p: P) => (
  <Svg {...p}>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <path d="M6.5 3v10" />
  </Svg>
);

export const PanelRight = (p: P) => (
  <Svg {...p}>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <path d="M9.5 3v10" />
  </Svg>
);

export const Pencil = (p: P) => (
  <Svg {...p}>
    <path d="M10.5 3.5l2 2L6 12H4v-2l6.5-6.5zM9.5 4.5l2 2" />
  </Svg>
);

export const Flame = (p: P) => (
  <Svg {...p}>
    <path d="M8 1.8c.4 2.4 3.9 4.3 3.9 8a3.9 3.9 0 0 1-7.8 0c0-1.7.8-2.9 1.7-3.7 0 1.4.6 2.3 1.5 2.7C6.9 6.3 7.2 4 8 1.8z" />
  </Svg>
);

// The gear from Lucide (ISC licence), drawn on its 24px grid.
export const Gear = (p: P) => (
  <Svg {...p}>
    <g transform="scale(0.6667)" strokeWidth={2.25}>
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
      <circle cx="12" cy="12" r="3" />
    </g>
  </Svg>
);

export function Spinner({ size = 14 }: { size?: number }) {
  return <span className="spinner" style={{ width: size, height: size }} aria-label="Working" />;
}

// The Tutortop mark: a lowercase t on a rounded square. Same drawing as build/logo.svg.
// The square takes the text colour, the t the page colour.
export const Logo = ({ size = 20, className }: P) => (
  <svg width={size} height={size} viewBox="0 0 120 120" className={className} aria-hidden="true">
    <rect width="120" height="120" rx="22" fill="currentColor" />
    <path fill="var(--paper)" d="M51 24H65.5V41H78.5V52.5H65.5V71C65.5 77.5 68.5 80.5 74 80.5C75.8 80.5 77.5 80.2 79 79.8L80.5 94C78 94.6 75 95 72 95C58 95 51 88 51 74V52.5H40V41C46.5 41 51 36.5 51 30Z" />
  </svg>
);
