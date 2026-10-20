import { useState } from "react";
import { Outlet } from "react-router-dom";
import { Menu } from "lucide-react";
import { Sidebar } from "@/components/layout/sidebar";
import { Rail } from "./rail";

const TasksLayout = () => {
  const [railOpen, setRailOpen] = useState(false);
  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <div className="hidden lg:block">
        <Sidebar />
      </div>

      <div className="hidden sm:block">
        <Rail />
      </div>

      {/* Mobile rail drawer */}
      {railOpen ? (
        <div className="fixed inset-0 z-40 sm:hidden">
          <div className="absolute inset-0 bg-ink-deep/40 animate-overlay-in" onClick={() => setRailOpen(false)} />
          <div className="absolute inset-y-0 left-0 w-[85vw] max-w-sm animate-drawer-in">
            <Rail onNavigate={() => setRailOpen(false)} />
          </div>
        </div>
      ) : null}

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-12 items-center border-b border-gray-200 px-3 sm:hidden">
          <button
            type="button"
            onClick={() => setRailOpen(true)}
            aria-label="Open tasks"
            className="rounded-lg p-2 text-ink-muted hover:bg-gray-100"
          >
            <Menu className="h-5 w-5" />
          </button>
        </div>
        <Outlet />
      </div>
    </div>
  );
};

export default TasksLayout;
