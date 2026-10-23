import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Analytics from "../pages/Analytics";

export const Route = createFileRoute("/analytics")({
  beforeLoad: requireAuth,
  component: Analytics,
});
