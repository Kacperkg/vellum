import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Settings from "../pages/Settings";

export const Route = createFileRoute("/settings")({
  beforeLoad: requireAuth,
  component: Settings,
});
