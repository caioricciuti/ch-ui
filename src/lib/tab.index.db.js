const dbName = "tabs_indexed_db";
const storeName = "tabs_store";

function initDB() {
  return new Promise((resolve, reject) => {
    const request = window.indexedDB.open(dbName, 1);

    request.onupgradeneeded = (event) => {
      let db = event.target.result;
      if (!db.objectStoreNames.contains(storeName)) {
        db.createObjectStore(storeName, { keyPath: "tab_id" });
      }
    };

    request.onerror = (event) => {
      reject("IndexedDB could not be opened: " + event.target.errorCode);
    };

    request.onsuccess = (event) => {
      resolve(event.target.result);
    };
  });
}

function readFromDB() {
  return new Promise(async (resolve, reject) => {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readonly");
    const store = transaction.objectStore(storeName);
    const request = store.getAll();

    request.onerror = (event) => {
      reject("Failed to retrieve data: " + event.target.errorCode);
    };

    request.onsuccess = (event) => {
      resolve(event.target.result);
    };
  });
}

async function writeToDB(tab) {
  try {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readwrite");
    const store = transaction.objectStore(storeName);
    tab.id = tab.id || tab.title;
    store.put(tab); // Just pass the whole object

    return new Promise((resolve, reject) => {
      transaction.oncomplete = () => resolve();
      transaction.onerror = (event) =>
        reject("Data could not be saved: " + event.target.errorCode);
    });
  } catch (error) {
    console.error("Error writing to DB:", error);
    throw error; // Rethrow or handle as necessary
  }
}

function deleteFromDB(tabId) {
  return new Promise(async (resolve, reject) => {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readwrite");
    const store = transaction.objectStore(storeName);
    const request = store.delete(tabId);

    request.onerror = (event) => {
      reject("Failed to delete data: " + event.target.errorCode);
    };

    request.onsuccess = () => {
      resolve();
    };
  });
}

function updateRecord(tabId, tab) {
  return new Promise(async (resolve, reject) => {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readwrite");
    const store = transaction.objectStore(storeName);
    const request = store.put(tab);

    request.onerror = (event) => {
      reject("Failed to update data: " + event.target.errorCode);
    };

    request.onsuccess = () => {
      resolve();
    };
  });
}

export { initDB, readFromDB, writeToDB, updateRecord, deleteFromDB };
