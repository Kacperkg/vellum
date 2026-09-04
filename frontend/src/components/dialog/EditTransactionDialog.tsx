import Input from "../Input";
import Select from "../Select";
import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function EditTransactionDialog({
  dialogOpen,
  setDialogOpen,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title="Edit Transaction"
      description="Log an expense or income."
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>Save changes</Button>
        </>
      }
    >
      <div className="flex flex-col w-full gap-4">
        <div className="w-full flex flex-col gap-2">
          <h1>Merchant</h1>
          <Input
            variant="string"
            placeholder="e.g Whole Foods"
            className="w-full"
          />
        </div>
        <div className="flex w-full gap-4">
          {/* Amount */}
          <div className="w-full flex flex-col gap-2">
            <h1>Amount</h1>
            <Input variant="number" placeholder="50.00" className="w-full" />
          </div>
          {/* Date */}
          <div className="w-full flex flex-col gap-2">
            <h1>Date</h1>
            <Input variant="date" className="w-full" />
          </div>
        </div>
        <div className="w-full flex gap-4">
          {/* Category */}
          <div className="w-full flex flex-col gap-2">
            <h1>Category</h1>
            <Select defaultValue="Category1" className="w-full">
              <option value="Category1">Category1</option>
              <option value="Category2">Category2</option>
            </Select>
          </div>
          {/* Type */}
          <div className="w-full flex flex-col gap-2">
            <h1>Type</h1>
            <Select defaultValue="expense" className="w-full">
              <option value="expense">Expense</option>
              <option value="income">Income</option>
            </Select>
          </div>
        </div>
        {/* Account */}
        <div className="w-full flex flex-col gap-2">
          <h1>Account</h1>
          <Select className="w-full">
            <option value="account1">Account1</option>
            <option value="account2">Account2</option>
          </Select>
        </div>
        {/* Notes */}
        <div className="w-full flex flex-col gap-2">
          <h1>Notes</h1>
          <Input variant="string" placeholder="optional" className="w-full" />
        </div>
      </div>
    </Dialog>
  );
}
