import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";
import useOrganizationStore from "@/stores/organization.store";
import OrganizationList from "@/components/OrganizationList";
import AddOrganizationDialog from "@/components/AddOrganizationDialog";
import OrganizationDetailDialog from "@/components/OrganizationDetailDialog";
import UpdateOrganizationDialog from "@/components/UpdateOrganizationDialog";
import { Search, Plus, ArrowUpDown, Tally5 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

import { Organization } from "@/types/types";

function OrganizationsPage() {
  const { organizations, fetchOrganizations, isLoading, error } =
    useOrganizationStore();
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isDetailDialogOpen, setIsDetailDialogOpen] = useState(false);
  const [isUpdateDialogOpen, setIsUpdateDialogOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState("");
  const [sortBy, setSortBy] = useState<"name" | "memberCount">("name");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc");

  useEffect(() => {
    fetchOrganizations();
  }, [fetchOrganizations]);

  useEffect(() => {
    if (error) {
      toast.error(error);
    }
  }, [error, toast]);

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

  const toggleSort = () => {
    if (sortBy === "name") {
      setSortBy("memberCount");
    } else {
      setSortBy("name");
    }
    setSortOrder(sortOrder === "asc" ? "desc" : "asc");
  };

  return (
    <Card className="container mx-auto my-6 max-w-8xl">
      <CardHeader>
        <CardTitle className="text-2xl font-bold">Organizations</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex justify-between items-center mb-6">
          <Button
            className="bg-orange-400/30 text-orange-600 hover:bg-orange-400/40"
            onClick={() => setIsAddDialogOpen(true)}
          >
            <Plus className="mr-2 h-4 w-4" /> Add Organization
          </Button>
          <div className="relative w-64">
            <Input
              className="pr-10"
              placeholder="Search organizations..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
            <Search className="absolute right-3 top-3" />
          </div>
        </div>

        {isLoading ? (
          <>
            <div className="grid gap-4 mb-4">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
            <div className="grid grid-cols-3 gap-4">
              <Skeleton className="h-48 w-full" />
              <Skeleton className="h-48 w-full" />
              <div className="flex flex-col gap-2">
                <Skeleton className="h-8 w-full mb-2" />
                <Skeleton className="h-8 w-full mb-2" />
                <Skeleton className="h-8 w-full mb-2" />
                <Skeleton className="h-12 w-full mb-2" />
              </div>
            </div>
          </>
        ) : filteredOrganizations.length > 0 ? (
          <>
            <div className="mb-4">
              <Button
                variant="outline"
                onClick={toggleSort}
                className="flex items-center"
              >
                <ArrowUpDown className="mr-2 h-4 w-4" />
                Sort by {sortBy === "name" ? "Name" : "Member Count"} (
                {sortOrder === "asc" ? "Ascending" : "Descending"})
              </Button>
            </div>
            <OrganizationList
              organizations={filteredOrganizations}
              onViewDetails={(org: Organization) => {
                useOrganizationStore.getState().setSelectedOrganization(org);
                setIsDetailDialogOpen(true);
              }}
              onEdit={(org: Organization) => {
                useOrganizationStore.getState().setSelectedOrganization(org);
                setIsUpdateDialogOpen(true);
              }}
              onDelete={(org: Organization) => {
                useOrganizationStore.getState().deleteOrganization(org._id);
              }}
            />
          </>
        ) : (
          <div className="text-center py-8">
            <Tally5 className="h-12 w-12 text-gray-500 mx-auto" />
            <p className="mt-2 text-xs mb-4 text-gray-500">
              No organizations found. Try adjusting your search or add a new
              organization.
            </p>
            <Button variant="link" onClick={() => setIsAddDialogOpen(true)}>
              <Plus className="mr-2 h-4 w-4" /> Add Organization
            </Button>
          </div>
        )}
      </CardContent>

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
    </Card>
  );
}

export default OrganizationsPage;
