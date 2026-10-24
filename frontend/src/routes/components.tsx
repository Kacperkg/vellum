import { createFileRoute } from "@tanstack/react-router";
import ComponentsDemo from "@/pages/ComponentsDemo";
import { requireAuth } from "@/features/auth";

export const Route = createFileRoute("/components")({
  beforeLoad: requireAuth,
  component: ComponentsDemo,
});
