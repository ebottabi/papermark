import { NavLink } from "react-router-dom";
import { LogOut } from "lucide-react";
import { cn } from "@/lib/utils";
import { useAuth } from "@/hooks/use-auth";
import { Avatar } from "@/components/ui";
import { NAV_ITEMS } from "./navigation";

export const Sidebar = ({ onNavigate }: { onNavigate?: () => void }) => {
  const { user, logout } = useAuth();
  const name = user?.name || user?.email || "You";

  return (
    <aside className="flex h-full w-[248px] flex-col border-r border-gray-200 bg-background-surface">
      <div className="flex h-16 items-center gap-2 px-5">
        <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-white">
          <span className="font-serif text-body-1 font-semibold">K</span>
        </div>
        <span className="text-h-3 font-semibold tracking-tight text-ink">Keen</span>
      </div>

      <nav className="scrollbar-thin flex-1 space-y-0.5 overflow-y-auto px-3 py-2">
        {NAV_ITEMS.map((item) => {
          const Icon = item.icon;
          return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              onClick={onNavigate}
              className={({ isActive }) =>
                cn(
                  "group relative flex items-center gap-3 rounded-lg px-3 py-2 text-body-2 font-medium transition-colors",
                  isActive ? "bg-accent-soft text-accent-ink" : "text-ink-muted hover:bg-gray-100 hover:text-ink",
                )
              }
            >
              {({ isActive }) => (
                <>
                  {isActive ? (
                    <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-r-full bg-accent" />
                  ) : null}
                  <Icon className="h-[18px] w-[18px]" />
                  {item.label}
                </>
              )}
            </NavLink>
          );
        })}
      </nav>

      <div className="border-t border-gray-200 p-3">
        <div className="flex items-center gap-3 rounded-lg px-2 py-2">
          <Avatar name={name} src={user?.picture || undefined} size="md" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-body-3 font-medium text-ink">{name}</p>
            <p className="truncate text-body-4 text-ink-faint">{user?.email}</p>
          </div>
          <button
            type="button"
            onClick={logout}
            aria-label="Sign out"
            className="rounded-lg p-1.5 text-ink-faint transition-colors hover:bg-gray-100 hover:text-crit"
          >
            <LogOut className="h-4 w-4" />
          </button>
        </div>
      </div>
    </aside>
  );
};
