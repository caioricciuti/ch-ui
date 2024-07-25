import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import CredentialList from "@/components/CredentialList";
import AddCredentialDialog from "@/components/AddCredentialDialog";
import CredentialDetailDialog from "@/components/CredentialDetailDialog";
import UpdateCredentialDialog from "@/components/UpdateCredentialDialog";
import { Search, Plus, ArrowUpDown, DatabaseZap } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

function CredentialsPage() {
  const {
    credentials,
    fetchCredentials,
    isLoading,
    error,
    setSelectedCredential,
  } = useClickHouseCredentialStore();
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isDetailDialogOpen, setIsDetailDialogOpen] = useState(false);
  const [isUpdateDialogOpen, setIsUpdateDialogOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [sortBy, setSortBy] = useState<"name" | "userCount">("name");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc");

  useEffect(() => {
    fetchCredentials();
  }, [fetchCredentials]);

  useEffect(() => {
    if (error) {
      toast.error(error);
    }
  }, [error]);

  const filteredCredentials = credentials
    .filter((cred) =>
      cred.name.toLowerCase().includes(searchTerm.toLowerCase())
    )
    .sort((a, b) => {
      if (sortBy === "name") {
        return sortOrder === "asc"
          ? a.name.localeCompare(b.name)
          : b.name.localeCompare(a.name);
      } else {
        return sortOrder === "asc"
          ? a.users.length - b.users.length
          : b.users.length - a.users.length;
      }
    });

  const toggleSort = () => {
    if (sortBy === "name") {
      setSortBy("userCount");
    } else {
      setSortBy("name");
    }
    setSortOrder(sortOrder === "asc" ? "desc" : "asc");
  };

  return (
    <Card className="container mx-auto my-6 max-w-8xl">
      <CardHeader>
        <CardTitle className="text-2xl font-bold">
          Click House Credentials
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex justify-between items-center mb-6">
          <Button
            className="bg-blue-400/30 text-blue-600 hover:bg-blue-400/40"
            onClick={() => setIsAddDialogOpen(true)}
          >
            <Plus className="mr-2 h-4 w-4" /> Add Credential
          </Button>
          <div className="relative w-64 items-center flex">
            <Input
              className="pr-10"
              placeholder="Search credentials..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
            <Search className=" right-3 top-2 absolute" />
          </div>
        </div>

        {isLoading ? (
          <div className="text-center py-8">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-gray-900 mx-auto"></div>
            <p className="mt-4">Loading credentials...</p>
          </div>
        ) : filteredCredentials.length > 0 ? (
          <>
            <div className="mb-4">
              <Button
                variant="outline"
                onClick={toggleSort}
                className="flex items-center"
              >
                <ArrowUpDown className="mr-2 h-4 w-4" />
                Sort by {sortBy === "name" ? "Name" : "User Count"} (
                {sortOrder === "asc" ? "Ascending" : "Descending"})
              </Button>
            </div>
            <CredentialList
              credentials={filteredCredentials}
              onViewDetails={(cred) => {
                setSelectedCredential(cred);
                setIsDetailDialogOpen(true);
              }}
              onEdit={(cred) => {
                useClickHouseCredentialStore;
                setSelectedCredential(cred);
                setIsUpdateDialogOpen(true);
              }}
              onDelete={(cred) => {
                useClickHouseCredentialStore
                  .getState()
                  .deleteCredential(cred._id);
              }}
            />
          </>
        ) : (
          <div className="text-center py-8">
            <DatabaseZap className="h-12 w-12 text-gray-500 mx-auto" />
            <p className="mt-2 text-xs mb-4 text-gray-500">
              No credentials found. Try adjusting your search or add a new
              credential.
            </p>
            <Button variant="link" onClick={() => setIsAddDialogOpen(true)}>
              <Plus className="mr-2 h-4 w-4" /> Add Credential
            </Button>
          </div>
        )}
      </CardContent>

      <AddCredentialDialog
        isOpen={isAddDialogOpen}
        onClose={() => setIsAddDialogOpen(false)}
      />
      <CredentialDetailDialog
        isOpen={isDetailDialogOpen}
        onClose={() => setIsDetailDialogOpen(false)}
      />
      <UpdateCredentialDialog
        isOpen={isUpdateDialogOpen}
        onClose={() => setIsUpdateDialogOpen(false)}
      />
    </Card>
  );
}

export default CredentialsPage;