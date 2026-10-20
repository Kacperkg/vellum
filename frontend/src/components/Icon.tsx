import type { SVGProps } from "react";

export type IconName =
  | "overview"
  | "accounts"
  | "transactions"
  | "budgets"
  | "analytics"
  | "goals"
  | "subscriptions"
  | "settings"
  | "logout"
  | "plus"
  | "search"
  | "filter"
  | "download"
  | "edit"
  | "trash"
  | "refresh"
  | "arrow"
  | "trend"
  | "spark";

type IconProps = SVGProps<SVGSVGElement> & { name: IconName };

const paths: Record<IconName, React.ReactNode> = {
  accounts: (
    <>
      <rect x="3" y="5" width="18" height="15" rx="2" />
      <path d="M3 9h18m-6 5h3M7 5V3h10v2" />
    </>
  ),
  overview: (
    <>
      <rect x="3" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" />
      <rect x="14" y="14" width="7" height="7" rx="1" />
    </>
  ),
  transactions: <path d="M7 7h11m0 0-3-3m3 3-3 3M17 17H6m0 0 3 3m-3-3 3-3" />,
  budgets: (
    <path d="M21 12a9 9 0 1 1-9-9v9h9Zm-6-9.5A9 9 0 0 1 21.5 9H15V2.5Z" />
  ),
  analytics: <path d="M4 19V5m0 14h16M7 15l4-4 3 2 5-6" />,
  goals: (
    <>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="5" />
      <circle cx="12" cy="12" r="1" fill="currentColor" stroke="none" />
    </>
  ),
  subscriptions: <path d="M4 8V4m0 0h4M4 4l3 3a8 8 0 1 1-2 8" />,
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.55V21h-4v-.08A1.7 1.7 0 0 0 8.97 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.6 15 1.7 1.7 0 0 0 3.08 14H3v-4h.08A1.7 1.7 0 0 0 4.6 8.97a1.7 1.7 0 0 0-.34-1.88l-.06-.06L7.03 4.2l.06.06A1.7 1.7 0 0 0 8.97 4.6 1.7 1.7 0 0 0 10 3.08V3h4v.08A1.7 1.7 0 0 0 15.03 4.6a1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.4 8.97 1.7 1.7 0 0 0 20.92 10H21v4h-.08A1.7 1.7 0 0 0 19.4 15Z" />
    </>
  ),
  logout: (
    <path d="M10 17l5-5-5-5m5 5H3m10-8h5a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-5" />
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-4-4" />
    </>
  ),
  filter: (
    <path d="M4 7h10m4 0h2M4 17h2m4 0h10M14 5v4M6 15v4M4 12h4m4 0h8M8 10v4" />
  ),
  download: <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 19h14" />,
  edit: <path d="m4 20 4-.8L19 8.2a2.1 2.1 0 0 0-3-3L5 16.2 4 20Z" />,
  trash: <path d="M4 7h16m-10 4v6m4-6v6M9 7l1-3h4l1 3m3 0-1 14H7L6 7" />,
  refresh: <path d="M4 8V4m0 0h4M4 4l3 3a8 8 0 1 1-2 8" />,
  arrow: <path d="M7 17 17 7m0 0h-7m7 0v7" />,
  trend: <path d="m3 17 6-6 4 4 8-9m0 0h-5m5 0v5" />,
  spark: (
    <path d="m12 3 1.2 3.8L17 8l-3.8 1.2L12 13l-1.2-3.8L7 8l3.8-1.2L12 3Zm6 10 .8 2.2L21 16l-2.2.8L18 19l-.8-2.2L15 16l2.2-.8L18 13ZM6 14l.8 2.2L9 17l-2.2.8L6 20l-.8-2.2L3 17l2.2-.8L6 14Z" />
  ),
};

export default function Icon({ name, className = "", ...props }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={`size-4 shrink-0 ${className}`}
      {...props}
    >
      {paths[name]}
    </svg>
  );
}
