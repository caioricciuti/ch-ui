import React from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import useAuthStore from "@/stores/user.store";
import useOrganizationStore from "@/stores/organization.store";
import {
  DatabaseZap,
  Users,
  Building,
  Calendar,
  Hash,
  AtSign,
  Mail,
  GlobeIcon,
} from "lucide-react";

import {
  getInitials,
  bgColorsByInitials,
  bgGradientByInitials,
} from "@/lib/helpers";

interface CredentialDetailDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const CredentialDetailDialog: React.FC<CredentialDetailDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { selectedCredential } = useClickHouseCredentialStore();
  const { allUsers } = useAuthStore();
  const { organizations } = useOrganizationStore();

  if (!selectedCredential) return null;

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString(undefined, {
      year: "numeric",
      month: "long",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  // Extract the IDs from selectedCredential.users
  const selectedUserIds = selectedCredential.users.map((user) => user._id);

  const credentialUsers = allUsers.filter((user) =>
    selectedUserIds.includes(user._id)
  );

  // Extract the IDs from selectedCredential.allowedOrganizations
  const selectedOrganizationIds = selectedCredential.allowedOrganizations.map(
    (org) => org._id
  );

  const credentialOrganizations = organizations.filter((org) =>
    selectedOrganizationIds.includes(org._id)
  );

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent
        className="sm:max-w-[600px]"
        onOpenAutoFocus={(e) => e.preventDefault()}
        aria-describedby="credential-details"
        aria-description="Detailed information about the ClickHouse credential"
      >
        <DialogHeader>
          <DialogTitle
            className={`text-2xl font-bold items-center gap-2 ${bgGradientByInitials(
              getInitials(selectedCredential.name)
            )} text-transparent bg-clip-text`}
          >
            {selectedCredential.name}
          </DialogTitle>
          <DialogDescription
            aria-description="Details about the ClickHouse credential"
            id="credential-details"
          >
            Detailed information about {selectedCredential.name} ClickHouse
            credential.
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[70vh] pr-4">
          <Card>
            <CardContent className="pt-6">
              <div className="flex items-center space-x-4 mb-4">
                <DatabaseZap className="h-10 w-10 text-blue-500" />
                <div>
                  <h3 className="text-lg font-semibold">
                    {selectedCredential.name}
                  </h3>
                  <div className="text-sm text-muted-foreground flex items-center gap-1">
                    <AtSign className="h-4 w-4" aria-hidden="true" />
                    <span aria-label="Credential host and port">
                      {selectedCredential.host}:{selectedCredential.port}
                    </span>
                  </div>
                </div>
              </div>

              <Separator className="my-4" />

              <div className="space-y-2">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="flex items-center gap-2 text-sm cursor-help">
                        <Hash className="h-4 w-4" aria-hidden="true" />
                        <span aria-label="Credential ID">
                          ID: {selectedCredential._id}
                        </span>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Unique identifier for the credential</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                <div className="flex items-center gap-2 text-sm">
                  <Users className="h-4 w-4" aria-hidden="true" />
                  <span aria-label="Number of users">Users: </span>
                  <Badge variant="secondary">
                    {selectedCredential.users.length}
                  </Badge>
                </div>

                <div className="flex items-center gap-2 text-sm">
                  <Building className="h-4 w-4" aria-hidden="true" />
                  <span aria-label="Number of allowed organizations">
                    Allowed Organizations:{" "}
                  </span>
                  <Badge variant="secondary">
                    {selectedCredential.allowedOrganizations.length}
                  </Badge>
                </div>

                <div className="flex items-center gap-2 text-sm">
                  <Calendar className="h-4 w-4" aria-hidden="true" />
                  <span aria-label="Creation date">
                    Created: {formatDate(selectedCredential.createdAt)}
                  </span>
                </div>

                <div className="flex items-center gap-2 text-sm">
                  <Calendar className="h-4 w-4" aria-hidden="true" />
                  <span aria-label="Last update date">
                    Updated: {formatDate(selectedCredential.updatedAt)}
                  </span>
                </div>
              </div>

              <Separator className="my-4" />

              <Accordion type="single" collapsible className="w-full">
                <AccordionItem value="users">
                  <AccordionTrigger>
                    <div className="flex items-center gap-2">
                      <Users className="h-4 w-4" />
                      Users ({credentialUsers.length})
                    </div>
                  </AccordionTrigger>
                  <AccordionContent>
                    <div className="space-y-4">
                      {credentialUsers.map((user) => (
                        <div
                          key={user._id}
                          className="flex items-center gap-3 border p-2.5 rounded-md"
                        >
                          <Avatar>
                            <AvatarFallback
                              className={`h-10 w-10 font-bold ${bgColorsByInitials(
                                getInitials(user?.name || "")
                              )}`}
                            >
                              {getInitials(user?.name || "")}
                            </AvatarFallback>
                          </Avatar>
                          <div>
                            <p className="text-sm font-medium">{user.name}</p>
                            <p className="text-xs text-muted-foreground flex items-center gap-1">
                              <Mail className="h-3 w-3" />
                              {user.email}
                            </p>
                          </div>
                        </div>
                      ))}
                    </div>
                  </AccordionContent>
                </AccordionItem>
                <AccordionItem value="organizations">
                  <AccordionTrigger>
                    <div className="flex items-center gap-2">
                      <Building className="h-4 w-4" />
                      Allowed Organizations ({credentialOrganizations.length})
                    </div>
                  </AccordionTrigger>
                  <AccordionContent>
                    <div className="space-y-4">
                      {credentialOrganizations.map((org) => (
                        <div
                          key={org._id}
                          className="flex items-center gap-3 border p-2.5 rounded-md"
                        >
                          <Avatar>
                            <AvatarFallback
                              className={`h-10 w-10 font-bold ${bgColorsByInitials(
                                getInitials(org?.name || "")
                              )}`}
                            >
                              {getInitials(org?.name || "")}
                            </AvatarFallback>
                          </Avatar>
                          <div>
                            <p className="text-sm font-medium">{org.name}</p>
                            <p className="text-xs text-muted-foreground flex items-center gap-1">
                              <GlobeIcon className="h-3 w-3" />
                              {org.slug}
                            </p>
                          </div>
                        </div>
                      ))}
                    </div>
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            </CardContent>
          </Card>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
};

export default CredentialDetailDialog;
