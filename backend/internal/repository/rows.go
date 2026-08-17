package repository

import "database/sql"

func mapsFromRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for index, value := range values {
			if bytes, ok := value.([]byte); ok {
				item[columns[index]] = string(bytes)
			} else {
				item[columns[index]] = value
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
