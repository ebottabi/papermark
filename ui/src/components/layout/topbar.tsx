import { Menu, Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui";
import { ROUTES } from "@/lib/constants";

export const Topbar = ({ onMenuClick }: { onMenuClick: () => void }) => {
  const navigate = useNavigate();
  return (
    <header className="flex h-16 shrink-0 items-center justify-between gap-3 border-b border-gray-200 bg-background-surface/80 px-4 backdrop-blur sm:px-6">
      <button
        type="button"
        onClick={onMenuClick}
        aria-label="Open menu"
        className="rounded-lg p-2 text-ink-muted hover:bg-gray-100 lg:hidden"
      >
        <Menu className="h-5 w-5" />
      </button>
      <div className="flex-1" />
      <Button size="sm" variant="accent" leftIcon={<Plus className="h-4 w-4" />} onClick={() => navigate(ROUTES.TASKS)}>
        New task
      </Button>
    </header>
  );
};
