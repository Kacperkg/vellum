import Input from "../Input";
import Select from "../Select";
import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function NewBudgetDialog({
  dialogOpen,
  setDialogOpen,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title="New Budget"
      description="Allocate a monthly limit to a category"
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>Create budget</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h1>Category</h1>
          <Select defaultValue="option1">
            <option value="option1">option1</option>
            <option value="option2">option2</option>
          </Select>
        </div>
        <div className="flex flex-col gap-2">
          <h1>Monthly Allocation</h1>
          <Input variant="number" placeholder="500"></Input>
        </div>
      </div>
    </Dialog>
  );
}
