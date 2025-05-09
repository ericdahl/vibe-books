package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/ericdahl/bookshelf/internal/model"
)

// BookStore defines the interface for database operations on books.
type BookStore interface {
	AddBook(book *model.Book) (int64, error)
	GetBooks() ([]model.Book, error)
	GetBookByID(id int64) (*model.Book, error)
	UpdateBookStatus(id int64, status model.BookStatus) error
	UpdateBookType(id int64, bookType model.BookType) error
	UpdateBookDetails(id int64, rating *int, comments *string, series *string, seriesIndex *int) error
	DeleteBook(id int64) error
}

// SQLiteBookStore implements the BookStore interface using SQLite.
type SQLiteBookStore struct {
	DB *sql.DB
}

// NewSQLiteBookStore creates a new SQLiteBookStore.
func NewSQLiteBookStore(db *sql.DB) *SQLiteBookStore {
	return &SQLiteBookStore{DB: db}
}

// AddBook inserts a new book into the database.
// It sets the book's ID after successful insertion.
func (s *SQLiteBookStore) AddBook(book *model.Book) (int64, error) {
	// Default status if not provided (though handler should ensure it)
	if book.Status == "" {
		book.Status = model.StatusWantToRead // Or Currently Reading as per initial request? Let's stick to Want to Read for now.
	} else if !book.Status.IsValid() {
		return 0, fmt.Errorf("invalid status: %s", book.Status)
	}

	if err := book.Validate(); err != nil {
		return 0, fmt.Errorf("validation failed: %w", err)
	}

	query := `
        INSERT INTO books (title, author, open_library_id, isbn, status, type, rating, comments, cover_url)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
    `
	slog.Info("SQL: Executing AddBook query",
		"title", book.Title,
		"author", book.Author,
		"openLibraryID", book.OpenLibraryID,
		"isbn", book.ISBN,
		"status", book.Status,
		"type", book.Type,
		"rating", book.Rating,
		"comments", book.Comments,
		"coverURL", book.CoverURL)
	stmt, err := s.DB.Prepare(query)
	if err != nil {
		slog.Error("SQL Error: Preparing AddBook statement failed", "error", err)
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(book.Title, book.Author, book.OpenLibraryID, book.ISBN, book.Status, book.Type, book.Rating, book.Comments, book.CoverURL)
	if err != nil {
		slog.Error("SQL Error: Executing AddBook statement failed", "error", err)
		// Consider checking for UNIQUE constraint violation specifically
		return 0, fmt.Errorf("failed to execute insert statement: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		slog.Error("SQL Error: Failed to get last insert ID", "error", err)
		return 0, fmt.Errorf("failed to retrieve last insert ID: %w", err)
	}
	book.ID = id // Set the ID on the original struct
	slog.Info("SQL: Successfully added book", "id", id)
	return id, nil
}

// GetBooks retrieves all books from the database.
func (s *SQLiteBookStore) GetBooks() ([]model.Book, error) {
	query := `SELECT id, title, author, open_library_id, isbn, status, type, rating, comments, cover_url, series, series_index FROM books ORDER BY title;`
	slog.Info("SQL: Executing GetBooks query")

	rows, err := s.DB.Query(query)
	if err != nil {
		slog.Error("SQL Error: Executing GetBooks query failed", "error", err)
		return nil, fmt.Errorf("failed to query books: %w", err)
	}
	defer rows.Close()

	books := []model.Book{}
	for rows.Next() {
		var book model.Book
		// Ensure pointers are used for nullable fields
		var rating sql.NullInt64
		var comments sql.NullString
		var coverURL sql.NullString
		var isbn sql.NullString
		var series sql.NullString
		var seriesIndex sql.NullInt64
		var bookType sql.NullString

		if err := rows.Scan(&book.ID, &book.Title, &book.Author, &book.OpenLibraryID, &isbn, 
			&book.Status, &bookType, &rating, &comments, &coverURL, &series, &seriesIndex); err != nil {
			slog.Error("SQL Error: Scanning book row failed", "error", err)
			return nil, fmt.Errorf("failed to scan book row: %w", err)
		}
		
		// Set type, defaulting to "book" if NULL or invalid
		if bookType.Valid {
			book.Type = model.BookType(bookType.String)
		}
		if !book.Type.IsValid() {
			book.Type = model.TypeBook
		}

		// Convert sql.Null types to pointers
		if isbn.Valid {
			book.ISBN = isbn.String
		}
		if rating.Valid {
			r := int(rating.Int64)
			book.Rating = &r
		}
		if comments.Valid {
			book.Comments = &comments.String
		}
		if coverURL.Valid {
			book.CoverURL = &coverURL.String
		}
		if series.Valid {
			book.Series = &series.String
		}
		if seriesIndex.Valid {
			si := int(seriesIndex.Int64)
			book.SeriesIndex = &si
		}

		books = append(books, book)
	}

	if err = rows.Err(); err != nil {
		slog.Error("SQL Error: Error during row iteration", "error", err)
		return nil, fmt.Errorf("error iterating book rows: %w", err)
	}

	slog.Info("SQL: Retrieved books", "count", len(books))
	return books, nil
}

// GetBookByID retrieves a single book by its ID.
func (s *SQLiteBookStore) GetBookByID(id int64) (*model.Book, error) {
	query := `SELECT id, title, author, open_library_id, isbn, status, type, rating, comments, cover_url, series, series_index FROM books WHERE id = ?;`
	slog.Info("SQL: Executing GetBookByID query", "id", id)

	row := s.DB.QueryRow(query, id)

	var book model.Book
	var rating sql.NullInt64
	var comments sql.NullString
	var coverURL sql.NullString
	var isbn sql.NullString
	var series sql.NullString
	var seriesIndex sql.NullInt64
	var bookType sql.NullString

	err := row.Scan(&book.ID, &book.Title, &book.Author, &book.OpenLibraryID, &isbn, 
		&book.Status, &bookType, &rating, &comments, &coverURL, &series, &seriesIndex)
	if err != nil {
		if err == sql.ErrNoRows {
			slog.Info("SQL: No book found", "id", id)
			return nil, fmt.Errorf("book with ID %d not found", id) // Consider a specific error type (e.g., ErrNotFound)
		}
		slog.Error("SQL Error: Scanning book row failed", "id", id, "error", err)
		return nil, fmt.Errorf("failed to scan book row for ID %d: %w", id, err)
	}
	
	// Set type, defaulting to "book" if NULL or invalid
	if bookType.Valid {
		book.Type = model.BookType(bookType.String)
	}
	if !book.Type.IsValid() {
		book.Type = model.TypeBook
	}

	// Convert sql.Null types to pointers
	if isbn.Valid {
		book.ISBN = isbn.String
	}
	if rating.Valid {
		r := int(rating.Int64)
		book.Rating = &r
	}
	if comments.Valid {
		book.Comments = &comments.String
	}
	if coverURL.Valid {
		book.CoverURL = &coverURL.String
	}
	if series.Valid {
		book.Series = &series.String
	}
	if seriesIndex.Valid {
		si := int(seriesIndex.Int64)
		book.SeriesIndex = &si
	}

	slog.Info("SQL: Retrieved book", "id", id)
	return &book, nil
}

// UpdateBookStatus updates the status of a specific book.
func (s *SQLiteBookStore) UpdateBookStatus(id int64, status model.BookStatus) error {
	if !status.IsValid() {
		return fmt.Errorf("invalid status provided: %s", status)
	}

	query := `UPDATE books SET status = ? WHERE id = ?;`
	slog.Info("SQL: Executing UpdateBookStatus query", "status", status, "id", id)

	stmt, err := s.DB.Prepare(query)
	if err != nil {
		slog.Error("SQL Error: Preparing UpdateBookStatus statement failed", "error", err)
		return fmt.Errorf("failed to prepare update status statement: %w", err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(status, id)
	if err != nil {
		slog.Error("SQL Error: Executing UpdateBookStatus statement failed", "error", err)
		return fmt.Errorf("failed to execute update status statement: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		slog.Error("SQL Error: Failed to get rows affected for UpdateBookStatus", "error", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		slog.Info("SQL: No book found to update status", "id", id)
		return fmt.Errorf("book with ID %d not found", id) // Consider ErrNotFound
	}

	slog.Info("SQL: Successfully updated status for book", "id", id)
	return nil
}

// UpdateBookType updates the type of a specific book.
func (s *SQLiteBookStore) UpdateBookType(id int64, bookType model.BookType) error {
	if !bookType.IsValid() {
		return fmt.Errorf("invalid book type provided: %s", bookType)
	}

	query := `UPDATE books SET type = ? WHERE id = ?;`
	slog.Info("SQL: Executing UpdateBookType query", "type", bookType, "id", id)

	stmt, err := s.DB.Prepare(query)
	if err != nil {
		slog.Error("SQL Error: Preparing UpdateBookType statement failed", "error", err)
		return fmt.Errorf("failed to prepare update type statement: %w", err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(bookType, id)
	if err != nil {
		slog.Error("SQL Error: Executing UpdateBookType statement failed", "error", err)
		return fmt.Errorf("failed to execute update type statement: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		slog.Error("SQL Error: Failed to get rows affected for UpdateBookType", "error", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		slog.Info("SQL: No book found to update type", "id", id)
		return fmt.Errorf("book with ID %d not found", id)
	}

	slog.Info("SQL: Successfully updated type for book", "id", id)
	return nil
}

// UpdateBookDetails updates the rating, comments, series info of a specific book.
// It handles NULL values correctly.
func (s *SQLiteBookStore) UpdateBookDetails(id int64, rating *int, comments *string, series *string, seriesIndex *int) error {
	// Validate rating if provided
	if rating != nil && (*rating < 1 || *rating > 10) {
		return fmt.Errorf("rating must be between 1 and 10")
	}

	query := `UPDATE books SET rating = ?, comments = ?, series = ?, series_index = ? WHERE id = ?;`
	slog.Info("SQL: Executing UpdateBookDetails query", "rating", rating, "comments", comments, "series", series, "seriesIndex", seriesIndex, "id", id)

	stmt, err := s.DB.Prepare(query)
	if err != nil {
		slog.Error("SQL Error: Preparing UpdateBookDetails statement failed", "error", err)
		return fmt.Errorf("failed to prepare update details statement: %w", err)
	}
	defer stmt.Close()

	// Handle potential nil values for parameters when passing to Exec
	var sqlRating interface{}
	if rating != nil {
		sqlRating = *rating
	} else {
		sqlRating = nil // This will be translated to NULL by the driver
	}

	var sqlComments interface{}
	if comments != nil {
		sqlComments = *comments
	} else {
		sqlComments = nil // This will be translated to NULL by the driver
	}
	
	var sqlSeries interface{}
	if series != nil {
		sqlSeries = *series
	} else {
		sqlSeries = nil
	}
	
	var sqlSeriesIndex interface{}
	if seriesIndex != nil {
		sqlSeriesIndex = *seriesIndex
	} else {
		sqlSeriesIndex = nil
	}

	res, err := stmt.Exec(sqlRating, sqlComments, sqlSeries, sqlSeriesIndex, id)
	if err != nil {
		slog.Error("SQL Error: Executing UpdateBookDetails statement failed", "error", err)
		return fmt.Errorf("failed to execute update details statement: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		slog.Error("SQL Error: Failed to get rows affected for UpdateBookDetails", "error", err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		slog.Info("SQL: No book found to update details", "id", id)
		return fmt.Errorf("book with ID %d not found", id) // Consider ErrNotFound
	}

	slog.Info("SQL: Successfully updated details for book", "id", id)
	return nil
}

// DeleteBook removes a book from the database by its ID.
func (s *SQLiteBookStore) DeleteBook(id int64) error {
	query := `DELETE FROM books WHERE id = ?;`

	result, err := s.DB.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete book: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("book with ID %d not found", id)
	}

	return nil
}
