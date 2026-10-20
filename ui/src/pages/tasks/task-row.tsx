import { NavLink } from "react-router-dom";
import { Archive, ArchiveRestore, Clock, MoreHorizontal, Star } from "lucide-react";
import { cn, timeAgo } from "@/lib/utils";
import { ROUTES } from "@/lib/constants";
import { Badge, RowMenu, StatusBadge, Tooltip, focusRing } from "@/components/ui";
import type { Status, TaskSummary } from "@/types";

// Waiting means the agent is blocked on the user, so it reads as "Needs you"
// rather than a passive "Waiting"; running carries a live pulse.
const RowStatus = ({ status }: { status: Status }) => {
  if (status === "waiting")
    return (
      <Badge tone="high">
        <span className="h-1.5 w-1.5 rounded-full bg-high animate-blink" />
        Needs you
      </Badge>
    );
  if (status === "running")
    return (
      <Badge tone="accent">
        <span className="h-1.5 w-1.5 rounded-full bg-accent animate-pulse" />
        Running
      </Badge>
    );
  return <StatusBadge status={status} />;
};

export const TaskRow = ({
  task,
  onNavigate,
  onToggleFavorite,
  onToggleArchive,
}: {
  task: TaskSummary;
  onNavigate?: () => void;
  onToggleFavorite: (t: TaskSummary) => void;
  onToggleArchive: (t: TaskSummary) => void;
}) => (
  <li className="group relative">
    <NavLink
      to={`${ROUTES.TASKS}/${task.id}`}
      onClick={onNavigate}
      className={({ isActive }) =>
        cn(
          "block rounded-lg py-2.5 pl-3 pr-14 transition-colors",
          isActive ? "bg-accent-soft" : "hover:bg-gray-100",
        )
      }
    >
      <p className="truncate text-body-2 font-medium text-ink">{task.prompt || "Untitled task"}</p>
      <div className="mt-1.5 flex items-center gap-1.5">
        <RowStatus status={task.status} />
        {task.scheduleId ? (
          <Tooltip label="Scheduled">
            <Clock className="h-3.5 w-3.5 shrink-0 text-ink-faint" />
          </Tooltip>
        ) : null}
        <span className="ml-auto shrink-0 text-body-4 text-ink-faint">{timeAgo(task.updatedAt)}</span>
      </div>
    </NavLink>

    {/* Action buttons sit beside the link, not inside it, to avoid nesting controls in an anchor. */}
    <div className="absolute right-2 top-2 flex items-center gap-0.5">
      <button
        type="button"
        aria-label={task.favorite ? "Remove favorite" : "Add favorite"}
        aria-pressed={task.favorite}
        onClick={() => onToggleFavorite(task)}
        className={cn(
          "inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-gray-200",
          task.favorite ? "text-high" : "text-ink-faint hover:text-ink-muted",
          focusRing,
        )}
      >
        <Star className={cn("h-4 w-4", task.favorite && "fill-current")} />
      </button>
      <RowMenu
        actions={[
          {
            label: task.archived ? "Unarchive" : "Archive",
            icon: task.archived ? <ArchiveRestore className="h-4 w-4" /> : <Archive className="h-4 w-4" />,
            onSelect: () => onToggleArchive(task),
          },
        ]}
        trigger={
          <button
            type="button"
            aria-label="Task actions"
            className={cn(
              "inline-flex h-7 w-7 items-center justify-center rounded-md text-ink-faint transition-colors hover:bg-gray-200 hover:text-ink data-[state=open]:bg-gray-200 data-[state=open]:text-ink",
              focusRing,
            )}
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
        }
      />
    </div>
  </li>
);
