import Input from "../Input";
import Dialog from "./Dialog";
import Button from "../Button";
import Select from "../Select";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function NewSubscriptionDialog({
  dialogOpen,
  setDialogOpen,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title="New Subscription"
      description="Track a recurring monthly charge."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>
            Create subscription
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h1>Service</h1>
          <Input variant="string" placeholder="Netflix" />
        </div>
        <div className="flex gap-4 items-center">
          <div className="flex flex-col gap-2 w-full">
            <h1>Category</h1>
            <Select defaultValue="option1">
              <option value="option1">option1</option>
              <option value="option2">option2</option>
            </Select>
          </div>
          <div className="flex flex-col gap-2 w-full">
            <h1>Monthly</h1>
            <Input variant="number" placeholder="9.99" />
          </div>
        </div>
        <div className="flex flex-col gap-2 w-full">
          <h1>Next renewal</h1>
          <Input variant="date" />
        </div>
      </div>
    </Dialog>
  );
}
