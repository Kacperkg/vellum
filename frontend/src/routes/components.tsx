import { createFileRoute } from "@tanstack/react-router";
import ComponentsDemo from "../pages/ComponentsDemo";
import { requireAuth } from "../lib/requireAuth";

export const Route = createFileRoute("/components")({
  beforeLoad: requireAuth,
  component: ComponentsDemo,
});
