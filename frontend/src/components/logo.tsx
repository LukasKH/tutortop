type Props = { size?: number; className?: string };

// The Tutortop mark: a lowercase t on a rounded square. Same drawing as build/logo.svg.
// The square takes the text colour, the t the page colour.
export const Logo = ({ size = 20, className }: Props) => (
  <svg width={size} height={size} viewBox="0 0 120 120" className={className} aria-hidden="true">
    <rect width="120" height="120" rx="22" fill="currentColor" />
    <path
      fill="var(--background)"
      d="M51 24H65.5V41H78.5V52.5H65.5V71C65.5 77.5 68.5 80.5 74 80.5C75.8 80.5 77.5 80.2 79 79.8L80.5 94C78 94.6 75 95 72 95C58 95 51 88 51 74V52.5H40V41C46.5 41 51 36.5 51 30Z"
    />
  </svg>
);
