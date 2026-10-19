import {
  Blocks,
  Bell,
  BookOpen,
  Brain,
  CalendarClock,
  FolderKanban,
  MessagesSquare,
  Settings,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { ROUTES } from "@/lib/constants";

export type NavItem = { to: string; label: string; icon: LucideIcon; end?: boolean };

export const NAV_ITEMS: NavItem[] = [
  { to: ROUTES.TASKS, label: "Tasks", icon: MessagesSquare },
  { to: ROUTES.UPDATES, label: "Updates", icon: Bell },
  { to: ROUTES.SCHEDULES, label: "Routines", icon: CalendarClock },
  { to: ROUTES.PROJECTS, label: "Projects", icon: FolderKanban },
  { to: ROUTES.KNOWLEDGE, label: "Knowledge", icon: BookOpen },
  { to: ROUTES.SKILLS, label: "Skills", icon: Sparkles },
  { to: ROUTES.MEMORIES, label: "Memories", icon: Brain },
  { to: ROUTES.CONNECTORS, label: "Connectors", icon: Blocks },
  { to: ROUTES.SETTINGS, label: "Settings", icon: Settings },
];
