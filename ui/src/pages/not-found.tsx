import { Link } from "react-router-dom";
import { ROUTES } from "@/lib/constants";
import { Button } from "@/components/ui";

const NotFound = () => (
  <div className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-4 text-center">
    <p className="font-mono text-h-1 font-semibold text-ink-faint">404</p>
    <h1 className="text-h-2 font-semibold text-ink">Page not found</h1>
    <Link to={ROUTES.TASKS}>
      <Button variant="secondary">Back to tasks</Button>
    </Link>
  </div>
);

export default NotFound;
