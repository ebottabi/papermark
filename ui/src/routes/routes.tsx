import { Navigate, Route, Routes } from "react-router-dom";
import { ROUTES } from "@/lib/constants";
import { ProtectedRoute, PublicRoute } from "@/components/auth/protected-route";
import { Layout } from "@/components/layout/layout";

import Login from "@/pages/auth/login";
import TasksLayout from "@/pages/tasks/tasks-layout";
import TaskStart from "@/pages/tasks/task-start";
import TaskChat from "@/pages/tasks/task-chat";
import Updates from "@/pages/updates/updates";
import Schedules from "@/pages/schedules/schedules";
import Projects from "@/pages/projects/projects";
import Knowledge from "@/pages/knowledge/knowledge";
import Skills from "@/pages/skills/skills";
import Memories from "@/pages/memories/memories";
import Connectors from "@/pages/connectors/connectors";
import Settings from "@/pages/settings/settings";
import NotFound from "@/pages/not-found";

export const AppRoutes = () => (
  <Routes>
    <Route element={<PublicRoute />}>
      <Route path={ROUTES.LOGIN} element={<Login />} />
    </Route>

    <Route element={<ProtectedRoute />}>
      {/* Tasks: full-height chat shell with its own sessions rail. */}
      <Route path={ROUTES.TASKS} element={<TasksLayout />}>
        <Route index element={<TaskStart />} />
        <Route path=":taskId" element={<TaskChat />} />
      </Route>

      {/* Everything else: standard padded app shell. */}
      <Route path="/" element={<Layout />}>
        <Route index element={<Navigate to={ROUTES.TASKS} replace />} />
        <Route path={ROUTES.UPDATES.slice(1)} element={<Updates />} />
        <Route path={ROUTES.SCHEDULES.slice(1)} element={<Schedules />} />
        <Route path={ROUTES.PROJECTS.slice(1)} element={<Projects />} />
        <Route path={ROUTES.KNOWLEDGE.slice(1)} element={<Knowledge />} />
        <Route path={ROUTES.SKILLS.slice(1)} element={<Skills />} />
        <Route path={ROUTES.MEMORIES.slice(1)} element={<Memories />} />
        <Route path={ROUTES.CONNECTORS.slice(1)} element={<Connectors />} />
        <Route path={ROUTES.SETTINGS.slice(1)} element={<Settings />} />
      </Route>
    </Route>

    <Route path="*" element={<NotFound />} />
  </Routes>
);
