import Logo from "./Logo";
import { Link, useNavigate } from "@tanstack/react-router";
import Card, { CardContent, CardFooter, CardHeader, CardTitle } from "./Card";
import Button from "./Button";
import { logout } from "../api/endpoints/auth";

const routes = [
  { label: "Overview", path: "/" },
  { label: "Transactions", path: "/transactions" },
  { label: "Budgets", path: "/budgets" },
  { label: "Analytics", path: "/analytics" },
  { label: "Goals", path: "/goals" },
  { label: "Subscriptions", path: "/subscriptions" },
  { label: "Settings", path: "/settings" },
  { label: "Components", path: "/components" },
] as const;

export default function SideNav() {
  const navigate = useNavigate();

  async function handleLogout(): Promise<void> {
    await logout();
    await navigate({
      to: "/login",
      replace: true,
    });
  }

  return (
    <nav className="pl-8 pt-8 pr-4 bg-sidebar border-r border-border flex flex-col justify-between">
      <div>
        <Logo />
        <div className="flex flex-col pt-8 text-muted-foreground">
          <h2 className="uppercase tracking-widest pb-2 text-md">Workspace</h2>
          <div className="flex flex-col text-md">
            {routes.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                activeProps={{
                  className:
                    "bg-sidebar-accent text-sidebar-accent-foreground before:absolute before:bg-accent before:w-0.5 before:h-5 before:-translate-y-1/2 before:top-1/2 before:left-0.5",
                }}
                inactiveProps={{
                  className:
                    "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                }}
                className="py-2 px-4 rounded-xl min-w-50 relative"
              >
                {item.label}
              </Link>
            ))}
          </div>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <Card>
          <CardHeader className="pb-1 pt-2">
            <CardTitle className="uppercase text-muted-foreground text-sm">
              month
            </CardTitle>
          </CardHeader>
          <CardContent className="py-0">
            <h1 className="text-2xl">3,000.00</h1>
            <p className="text-muted-foreground text-sm">of 4,180 budget</p>
          </CardContent>
          <CardFooter>
            <div className="w-full h-1 bg-muted-foreground rounded-md"></div>
          </CardFooter>
        </Card>
        <div className="flex justify-start items-center py-4">
          <p>icon</p>
          <Button
            variant="ghost"
            className="text-muted-foreground"
            onClick={handleLogout}
          >
            Sign out
          </Button>
        </div>
      </div>
    </nav>
  );
}
