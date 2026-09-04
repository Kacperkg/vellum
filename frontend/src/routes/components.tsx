import { createFileRoute } from "@tanstack/react-router";
import ComponentsDemo from "../pages/ComponentsDemo";

export const Route = createFileRoute("/components")({
  component: ComponentsDemo,
});
