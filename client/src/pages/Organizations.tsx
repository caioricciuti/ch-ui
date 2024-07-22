// src/pages/OrganizationsPage.tsx
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useToast } from "@/components/ui/use-toast";
import useOrganizationStore from "@/stores/organization.store";
import OrganizationList from "@/components/OrganizationList";
import AddOrganizationDialog from "@/components/AddOrganizationDialog";
import OrganizationDetailDialog from "@/components/OrganizationDetailDialog";
import UpdateOrganizationDialog from "@/components/UpdateOrganizationDialog";
import CredentialsDialog from "@/components/CredentialsDialog";
import { Search, Plus, ArrowUpAZ, ArrowDownAZ } from "lucide-react";

function OrganizationsPage() {
  const { organizations, fetchOrganizations, isLoading, error } =
    useOrganizationStore();
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isDetailDialogOpen, setIsDetailDialogOpen] = useState(false);
  const [isUpdateDialogOpen, setIsUpdateDialogOpen] = useState(false);
  const [isCredentialsDialogOpen, setIsCredentialsDialogOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [currentPage, setCurrentPage] = useState(1);
  const [sortBy, setSortBy] = useState<"name" | "memberCount">("name");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc");
  const { toast } = useToast();

  const itemsPerPage = 10;

  useEffect(() => {
    fetchOrganizations();
  }, [fetchOrganizations]);

  useEffect(() => {
    if (error) {
      toast({
        title: "Error",
        description: error,
        variant: "destructive",
      });
    }
  }, [error, toast]);

  useEffect(() => {
    setCurrentPage(1);
  }, [searchTerm]);

  const filteredOrganizations = organizations
    .filter((org) => org.name.toLowerCase().includes(searchTerm.toLowerCase()))
    .sort((a, b) => {
      if (sortBy === "name") {
        return sortOrder === "asc"
          ? a.name.localeCompare(b.name)
          : b.name.localeCompare(a.name);
      } else {
        return sortOrder === "asc"
          ? a.members.length - b.members.length
          : b.members.length - a.members.length;
      }
    });

  const paginatedOrganizations = filteredOrganizations.slice(
    (currentPage - 1) * itemsPerPage,
    currentPage * itemsPerPage
  );

  const totalPages = Math.ceil(filteredOrganizations.length / itemsPerPage);

  return (
    <div className="container mx-auto p-4">
      <h1 className="text-3xl font-bold mb-6">Organizations</h1>
      <div className="flex justify-between items-center mb-6">
        <>
          <Button
            className="bg-orange-400/30  text-orange-600 hover:bg-orange-400/40"
            onClick={() => setIsAddDialogOpen(true)}
          >
            <Plus className="mr-2" /> Add Organization
          </Button>
          <div className="relative">
            <Input
              className="max-w-xs pr-10"
              placeholder="Search organizations..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
            <Search className="absolute right-2 top-2.5 text-neutral-400" />
          </div>
        </>
      </div>
      <div className="mb-6 flex items-center">
        <span className="mr-2">Sort by:</span>
        <Button
          variant="outline"
          onClick={() => setSortBy("name")}
          className={`mr-2 ${
            sortBy === "name" ? "bg-primary text-primary-foreground" : ""
          }`}
        >
          Name
        </Button>
        <Button
          variant="outline"
          onClick={() => setSortBy("memberCount")}
          className={`mr-2 ${
            sortBy === "memberCount" ? "bg-primary text-primary-foreground" : ""
          }`}
        >
          Member Count
        </Button>
        <Button
          variant="outline"
          onClick={() => setSortOrder(sortOrder === "asc" ? "desc" : "asc")}
          className="flex items-center"
        >
          {sortOrder === "asc" ? (
            <ArrowUpAZ className="mr-1" />
          ) : (
            <ArrowDownAZ className="mr-1" />
          )}
          {sortOrder === "asc" ? "Ascending" : "Descending"}
        </Button>
      </div>
      {isLoading ? (
        <p>Loading organizations...</p>
      ) : (
        <>
          <OrganizationList
            organizations={paginatedOrganizations}
            onViewDetails={(org) => {
              useOrganizationStore.getState().setSelectedOrganization(org);
              setIsDetailDialogOpen(true);
            }}
            onEdit={(org) => {
              useOrganizationStore.getState().setSelectedOrganization(org);
              setIsUpdateDialogOpen(true);
            }}
            onDelete={(org) => {
              if (
                window.confirm(`Are you sure you want to delete ${org.name}?`)
              ) {
                useOrganizationStore.getState().deleteOrganization(org._id);
              }
            }}
            onManageCredentials={(org) => {
              useOrganizationStore.getState().setSelectedOrganization(org);
              setIsCredentialsDialogOpen(true);
            }}
          />
          <div className="mt-6 flex justify-center items-center">
            <Button
              onClick={() => setCurrentPage((prev) => Math.max(prev - 1, 1))}
              disabled={currentPage === 1}
              variant="ghost"
              className="mr-2"
            >
              Previous
            </Button>
            <span>
              Page {currentPage} of {totalPages}
            </span>
            <Button
              onClick={() =>
                setCurrentPage((prev) => Math.min(prev + 1, totalPages))
              }
              disabled={currentPage === totalPages}
              variant="ghost"
              className="ml-2"
            >
              Next
            </Button>
          </div>
        </>
      )}
      <AddOrganizationDialog
        isOpen={isAddDialogOpen}
        onClose={() => setIsAddDialogOpen(false)}
      />
      <OrganizationDetailDialog
        isOpen={isDetailDialogOpen}
        onClose={() => setIsDetailDialogOpen(false)}
      />
      <UpdateOrganizationDialog
        isOpen={isUpdateDialogOpen}
        onClose={() => setIsUpdateDialogOpen(false)}
      />
      <CredentialsDialog
        isOpen={isCredentialsDialogOpen}
        onClose={() => setIsCredentialsDialogOpen(false)}
      />
    </div>
  );
}

export default OrganizationsPage;
