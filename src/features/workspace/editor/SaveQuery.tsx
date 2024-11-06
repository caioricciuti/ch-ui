import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "sonner";
import { Save, Loader2 } from "lucide-react";
import useAppStore from "@/store";
import { genTabId } from "@/lib/utils";

interface SaveQueryDialogProps {
  query: string;
  onSave?: () => void;
}

export default function SaveQueryDialog({
  query,
  onSave,
}: SaveQueryDialogProps) {
  const { runQuery, credential } = useAppStore();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [formData, setFormData] = useState({
    name: "",
    description: "",
  });

  const handleSave = async () => {
    if (!formData.name.trim()) {
      toast.error("Please enter a query name");
      return;
    }

    setLoading(true);
    try {
      const queryId = genTabId();
      const currentTime = new Date().toISOString();

      await runQuery(`
        INSERT INTO CH_UI.saved_queries 
        (id, name, query, created_at, updated_at, owner)
        VALUES (
          '${queryId}',
          '${formData.name.replace(/'/g, "''")}',
          '${query.replace(/'/g, "''")}',
          parseDateTimeBestEffort('${currentTime}'),
          parseDateTimeBestEffort('${currentTime}'),
          '${credential.username}'
        )
      `);

      toast.success("Query saved successfully!");
      setOpen(false);
      setFormData({ name: "", description: "" });

      onSave?.();
    } catch (error) {
      console.error("Error saving query:", error);
      toast.error("Failed to save query. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm" className="gap-2">
          <Save className="h-4 w-4" />
          Save Query
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Save Query</DialogTitle>
        </DialogHeader>
        <div className="space-y-6 py-4">
          <div className="space-y-2">
            <Label htmlFor="name">Query Name</Label>
            <Input
              id="name"
              placeholder="Enter a name for your query"
              value={formData.name}
              onChange={(e) =>
                setFormData((prev) => ({ ...prev, name: e.target.value }))
              }
              className="col-span-3"
            />
          </div>

          <div className="space-y-2">
            <Label>Preview</Label>
            <div className="rounded-md bg-muted p-4">
              <pre className="text-xs font-mono whitespace-pre-wrap break-all">
                {query.length > 200 ? `${query.slice(0, 200)}...` : query}
              </pre>
            </div>
          </div>
        </div>
        <DialogFooter className="sm:justify-between">
          <Button
            type="button"
            variant="outline"
            onClick={() => setOpen(false)}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            onClick={handleSave}
            disabled={loading || !formData.name.trim()}
            className="gap-2"
          >
            {loading ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                Saving...
              </>
            ) : (
              <>
                <Save className="h-4 w-4" />
                Save Query
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
