import { useState } from "react";
import Button from "../components/Button";
import Card, {
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "../components/Card";
import Input from "../components/Input";
import Select from "../components/Select";
import DeleteDialog from "../components/dialog/DeleteDialog";
import NewTransactionDialog from "../components/dialog/NewTransactionDialog";
import EditTransactionDialog from "../components/dialog/EditTransactionDialog";
import NewBudgetDialog from "../components/dialog/NewBudgetDialog";
import NewGoalDialog from "../components/dialog/NewGoalDialog";
import NewSubscriptionDialog from "../components/dialog/NewSubscriptionDialog";
import NewAccountDialog from "../components/dialog/NewAccountDialog";

export default function ComponentsDemo() {
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteTransDialogOpen, setDeleteTransDialogOpen] = useState(false);

  const [addBudgetDialogOpen, setAddBudgetDialogOpen] = useState(false);
  const [deleteBudgetDialogOpen, setDeleteBudgetDialogOpen] = useState(false);

  const [goalDialogOpen, setAddGoalDialogOpen] = useState(false);

  const [subDialogOpen, setAddSubDialogOpen] = useState(false);

  const [accountDialogOpen, setAddAccountDialogOpen] = useState(false);

  return (
    <main className="p-8">
      <h1 className="text-2xl font-semibold">Component demos</h1>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Buttons</h2>
        <div className="flex flex-wrap items-center gap-4">
          <Button variant="primary" size="sm">
            Primary
          </Button>
          <Button variant="secondary" size="md">
            Secondary
          </Button>
          <Button variant="destructive" size="lg">
            Destructive
          </Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>

          <Button onClick={() => setAddDialogOpen(true)}>
            Add Transaction
          </Button>
          <Button onClick={() => setEditDialogOpen(true)}>
            Edit Transaction
          </Button>
          <Button onClick={() => setDeleteTransDialogOpen(true)}>
            Delete Transaction
          </Button>

          <Button onClick={() => setAddBudgetDialogOpen(true)}>
            Add Budget
          </Button>
          <Button onClick={() => setDeleteBudgetDialogOpen(true)}>
            Delete Budget
          </Button>

          <Button onClick={() => setAddGoalDialogOpen(true)}>Add Goal</Button>

          <Button onClick={() => setAddSubDialogOpen(true)}>Add Sub</Button>

          <Button onClick={() => setAddAccountDialogOpen(true)}>Add Acc</Button>
        </div>
      </section>

      <NewTransactionDialog
        dialogOpen={addDialogOpen}
        setDialogOpen={setAddDialogOpen}
      />
      <EditTransactionDialog
        dialogOpen={editDialogOpen}
        setDialogOpen={setEditDialogOpen}
      />
      <DeleteDialog
        dialogOpen={deleteTransDialogOpen}
        setDialogOpen={setDeleteTransDialogOpen}
        type="Transaction"
        input="item"
      />

      <DeleteDialog
        dialogOpen={deleteBudgetDialogOpen}
        setDialogOpen={setDeleteBudgetDialogOpen}
        type="Budget"
        input="item2"
      />
      <NewBudgetDialog
        dialogOpen={addBudgetDialogOpen}
        setDialogOpen={setAddBudgetDialogOpen}
      />

      <NewGoalDialog
        dialogOpen={goalDialogOpen}
        setDialogOpen={setAddGoalDialogOpen}
      />

      <NewSubscriptionDialog
        dialogOpen={subDialogOpen}
        setDialogOpen={setAddSubDialogOpen}
      />

      <NewAccountDialog
        dialogOpen={accountDialogOpen}
        setDialogOpen={setAddAccountDialogOpen}
      />

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Form controls</h2>
        <div className="flex w-fit flex-col gap-4">
          <Input type="text" placeholder="Text" />
          <Input variant="email" placeholder="Email" />
          <Input variant="password" placeholder="Password" />
          <Input variant="number" placeholder="Number" />
          <Select defaultValue="">
            <option value="" disabled>
              Select an option
            </option>
            <option value="one">One</option>
            <option value="two">Two</option>
          </Select>
          <Input variant="date" />
        </div>
      </section>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Card</h2>
        <Card className="w-80">
          <CardHeader>
            <CardTitle>Example card</CardTitle>
            <CardDescription>
              A quick preview of the card component.
            </CardDescription>
          </CardHeader>
          <CardContent>Cards can contain any content or controls.</CardContent>
          <CardFooter>
            <Button variant="outline">Cancel</Button>
            <Button>Continue</Button>
          </CardFooter>
        </Card>
      </section>
    </main>
  );
}
