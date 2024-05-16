import { toast } from "sonner";

const downloadCsv = (data, fileName, type) => {
  const dateTime = new Date();
  const dateUTC = dateTime.toISOString();
  fileName = `${fileName}-${dateUTC}`;
  if (!data || data.length === 0) {
    toast.error("No data to download");
  }
  try {
    if (type === "json") {
      const json = JSON.stringify(data, null, 2);
      const blob = new Blob([json], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${fileName}.json`;
      a.click();
      URL.revokeObjectURL(url);
    }
    if (type === "csv") {
      // Get the keys (column headers) from the first object in the data array
      const headers = Object.keys(data[0]);

      // Convert from JSON to CSV then download
      const csv = [
        headers.join(","),
        ...data.map((row) => headers.map((header) => row[header]).join(",")),
      ].join("\n");

      const blob = new Blob([csv], { type: "text/csv" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${fileName}.csv`;
      a.click();
      URL.revokeObjectURL(url);
    }
  } catch (error) {
    console.error(error);
  }
};

export default downloadCsv;
