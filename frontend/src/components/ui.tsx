import type { ReactNode } from "react";

export function cn(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

export function GlassCard({
  children, className, hover, onClick,
}: { children: ReactNode; className?: string; hover?: boolean; onClick?: () => void }) {
  return (
    <div
      className={cn("glass p-5", hover && "glass-hover cursor-pointer", className)}
      onClick={onClick}
      onKeyDown={onClick ? (e) => e.key === "Enter" && onClick() : undefined}
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      {children}
    </div>
  );
}

export function Button({
  children, onClick, variant = "primary", type = "button", className, disabled,
}: {
  children: ReactNode; onClick?: () => void;
  variant?: "primary" | "ghost" | "danger"; type?: "button" | "submit";
  className?: string; disabled?: boolean;
}) {
  const styles = {
    primary: "bg-primary text-primary-fg hover:opacity-90",
    ghost: "border border-card-border text-foreground hover:bg-card",
    danger: "bg-danger text-white hover:opacity-90",
  }[variant];
  return (
    <button
      type={type}
      disabled={disabled}
      onClick={onClick}
      className={cn("rounded-lg px-4 py-2 text-sm transition-opacity min-h-[36px]", styles, disabled && "opacity-50", className)}
    >
      {children}
    </button>
  );
}

export function Badge({ children, color }: { children: ReactNode; color?: string }) {
  return (
    <span
      className="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium"
      style={color ? { background: `${color}22`, color } : { background: "var(--card-border)" }}
    >
      {children}
    </span>
  );
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cn(
        "rounded-lg border border-card-border bg-card px-3 py-2 text-sm text-foreground placeholder:text-muted",
        props.className,
      )}
    />
  );
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(
        "rounded-lg border border-card-border bg-card px-2 py-2 text-sm text-foreground",
        props.className,
      )}
    />
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-lg bg-card-border", className)} />;
}

export function Empty({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
      <svg width="72" height="48" viewBox="0 0 72 48" fill="none" aria-hidden>
        <circle cx="12" cy="12" r="5" stroke="var(--lat-6)" strokeWidth="1.5" />
        <circle cx="60" cy="36" r="5" stroke="var(--lat-6)" strokeWidth="1.5" />
        <path d="M16 15 C 32 26, 44 24, 55 33" stroke="var(--lat-6)" strokeWidth="1.5" strokeDasharray="3 3" />
      </svg>
      <div className="text-sm font-medium">{title}</div>
      {hint && <div className="text-xs text-muted">{hint}</div>}
      {action}
    </div>
  );
}

export function StatusDot({ online }: { online: boolean }) {
  const color = online ? "var(--success)" : "var(--lat-6)";
  return (
    <span
      className="inline-block h-2 w-2 rounded-full"
      style={{ background: color, boxShadow: `0 0 0 3px color-mix(in srgb, ${color} 24%, transparent)` }}
      aria-label={online ? "在线" : "离线"}
    />
  );
}
