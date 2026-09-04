import Input from "../Input";
import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function NewGoalDialog({
  dialogOpen,
  setDialogOpen,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title="New Goal"
      description="Set a target and track it patiently."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>Create goal</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h1>Name</h1>
          <Input variant="string" placeholder="Emergency Fund" />
        </div>
        <div className="flex gap-4 items-center">
          <div className="flex flex-col gap-2 w-full">
            <h1>Target</h1>
            <Input variant="number" placeholder="15000" />
          </div>
          <div className="flex flex-col gap-2 w-full">
            <h1>Saved so far</h1>
            <Input variant="number" placeholder="0" />
          </div>
        </div>
        <div className="flex gap-4">
          <div className="flex flex-col gap-2 w-full">
            <h1>Monthly Contribution</h1>
            <Input variant="number" placeholder="500" />
          </div>
          <div className="flex flex-col gap-2 w-full">
            <h1>Target Date</h1>
            <Input variant="date" />
          </div>
        </div>
      </div>
    </Dialog>
  );
}
