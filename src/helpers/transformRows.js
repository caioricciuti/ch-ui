const transformRows = (previewData) => {
  if (!previewData) {
    return previewData
  }

  let data = previewData.map((row) => {
    let newRow = {}
    Object.keys(row).forEach((key) => {
      let value = row[key]
      // type checks
      if (
        typeof value === "object" &&
        !Array.isArray(value) &&
        value !== null
      ) {
        newRow[key] = JSON.stringify(value)
      } else if (Array.isArray(value)) {
        newRow[key] = JSON.stringify(value)
      } else {
        newRow[key] = row[key]
      }
    })
    return newRow
  })
  return data
}

export default transformRows
