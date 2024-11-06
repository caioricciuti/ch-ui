import { useEffect } from "react";
import useAppStore from "@/store";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Loader2,
  Database,
  CheckCircle,
  XCircle,
  Save,
  Users,
  Clock,
  AlertTriangle,
} from "lucide-react";
import ConfirmationDialog from "@/components/common/ConfirmationDialog";
import { useState } from "react";

export default function ActivateSavedQueries() {
  const {
    isAdmin,
    savedQueries: {
      isActivating,
      isDeactivating,
      isSavedQueriesActive,
      isCheckingStatus,
      error,
      totalQueries = 0,
      activeUsers = 0,
      lastUsed = null,
      storageUsed = "0 KB",
    },
    activateSavedQueries,
    deactivateSavedQueries,
    checkSavedQueriesStatus,
  } = useAppStore();

  const [isConfirmOpen, setIsConfirmOpen] = useState(false);

  useEffect(() => {
    checkSavedQueriesStatus();
    // Refresh status every 5 minutes
    const interval = setInterval(checkSavedQueriesStatus, 300000);
    return () => clearInterval(interval);
  }, []);

  const handleConfirm = async () => {
    setIsConfirmOpen(false);
    if (isSavedQueriesActive) {
      await deactivateSavedQueries();
    } else {
      await activateSavedQueries();
    }
  };

  if (!isAdmin) {
    return (
      <Alert variant="destructive" className="max-w-md">
        <XCircle className="h-4 w-4" />
        <AlertTitle>Access Denied</AlertTitle>
        <AlertDescription>
          You need administrator privileges to access this feature.
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Database className="h-5 w-5" />
            <CardTitle>Saved Queries</CardTitle>
          </div>
          {isSavedQueriesActive && (
            <Badge variant="outline" className="flex items-center space-x-1">
              <Save className="h-3 w-3 mr-1" />
              {totalQueries} {totalQueries === 1 ? "Query" : "Queries"} Saved
            </Badge>
          )}
        </div>
        <CardDescription>
          Manage the saved queries feature for all users
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        {isCheckingStatus ? (
          <div className="flex items-center justify-center p-4 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin mr-2" />
            <span>Checking status...</span>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                {isSavedQueriesActive ? (
                  <Badge
                    variant="success"
                    className="flex items-center space-x-1 bg-green-500/40"
                  >
                    <CheckCircle className="h-3 w-3" />
                    <span>Active</span>
                  </Badge>
                ) : (
                  <Badge
                    variant="destructive"
                    className="flex items-center space-x-1"
                  >
                    <XCircle className="h-3 w-3" />
                    <span>Inactive</span>
                  </Badge>
                )}
              </div>
              {isSavedQueriesActive && (
                <Badge
                  variant="secondary"
                  className="flex items-center space-x-1"
                >
                  <Save className="h-3 w-3 mr-1" />
                  {storageUsed} Used
                </Badge>
              )}
            </div>

            {isSavedQueriesActive && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="flex items-center space-x-2 text-sm">
                  <Users className="h-4 w-4 text-muted-foreground" />
                  <span>
                    {activeUsers} active {activeUsers === 1 ? "user" : "users"}
                  </span>
                </div>
                {lastUsed && (
                  <div className="flex items-center space-x-2 text-sm">
                    <Clock className="h-4 w-4 text-muted-foreground" />
                    <span>Last used {new Date(lastUsed).toLocaleString()}</span>
                  </div>
                )}
              </div>
            )}
          </>
        )}

        {error && (
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
      </CardContent>

      <CardFooter>
        <Button
          onClick={() => setIsConfirmOpen(true)}
          className="w-full"
          variant={isSavedQueriesActive ? "destructive" : "default"}
          disabled={isActivating || isDeactivating}
        >
          {(isActivating || isDeactivating) && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          {isSavedQueriesActive ? "Deactivate" : "Activate"} Saved Queries
        </Button>
      </CardFooter>

      <ConfirmationDialog
        title={
          isSavedQueriesActive
            ? "Deactivate Saved Queries"
            : "Activate Saved Queries"
        }
        description={
          isSavedQueriesActive
            ? `This action will remove the saved queries table and disable the saved queries feature for all users. 
               ${
                 totalQueries > 0
                   ? `<br/><br/><span class="font-bold text-destructive">WARNING: ${totalQueries} saved ${
                       totalQueries === 1 ? "query" : "queries"
                     } will be permanently deleted!</span>`
                   : ""
               }`
            : "This action will create the necessary tables and enable the saved queries feature for all users."
        }
        isOpen={isConfirmOpen}
        onClose={() => setIsConfirmOpen(false)}
        onConfirm={handleConfirm}
      />
    </Card>
  );
}
