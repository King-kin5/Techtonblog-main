package main

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Declare the adminPassword as a package-level variable
var adminPassword string

func init() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	// Set admin password from environment variable
	adminPassword = os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		log.Fatal("ADMIN_PASSWORD not set in .env file")
	}
}

// Post struct for blog posts
type Block struct {
    Type    string `json:"type"`    // e.g., "paragraph" or "image"
    Content string `json:"content"` // Content for paragraph or image URL
}

type Post struct {
    ID        int       `json:"id"`
    Title     string    `json:"title"`
    Blocks    []Block   `json:"blocks"`
    ImageData string    `json:"image_data"` // Field to store the image in base64 format
    CreatedAt time.Time `json:"created_at"`
}


var posts []Post // Holds all blog posts

func savePosts() error {
    data, err := json.MarshalIndent(posts, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile("data/posts.json", data, 0644)
}

func loadPosts() error {
    file, err := os.ReadFile("data/posts.json")
    if err != nil {
        return err
    }
    return json.Unmarshal(file, &posts)
}


// Handlers
func mainHandler(c echo.Context) error {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	isAdmin := isAuthenticated(c)

	data := struct {
		Posts   []Post
		IsAdmin bool
	}{
		Posts:   posts,
		IsAdmin: isAdmin,
	}

	return tmpl.Execute(c.Response().Writer, data)
}

func adminHandler(c echo.Context) error {
	if !isAuthenticated(c) {
		return c.Redirect(302, "/login")
	}

	tmpl := template.Must(template.ParseFiles("templates/admin.html"))
	data := struct {
		Posts []Post
	}{
		Posts: posts,
	}
	return tmpl.Execute(c.Response().Writer, data)
}

func postHandler(c echo.Context) error {
	tmpl := template.Must(template.ParseFiles("templates/post.html"))
	id, _ := strconv.Atoi(c.QueryParam("id"))
	for _, post := range posts {
		if post.ID == id {
			return tmpl.Execute(c.Response().Writer, post)
		}
	}
	return echo.NewHTTPError(404, "Post not found")
}

func loginHandler(c echo.Context) error {
	if c.Request().Method == echo.POST {
		password := c.FormValue("password")
		if password == adminPassword {
			cookie := new(http.Cookie)
			cookie.Name = "isAdmin"
			cookie.Value = "true"
			cookie.Path = "/"
			cookie.HttpOnly = true
			c.SetCookie(cookie)

			return c.Redirect(302, "/admin")
		}
		return echo.NewHTTPError(401, "Invalid password")
	}

	tmpl := template.Must(template.ParseFiles("templates/login.html"))
	return tmpl.Execute(c.Response().Writer, nil)
}

func logoutHandler(c echo.Context) error {
	cookie := new(http.Cookie)
	cookie.Name = "isAdmin"
	cookie.Value = ""
	cookie.Path = "/"
	cookie.MaxAge = -1
	c.SetCookie(cookie)

	return c.Redirect(302, "/login")
}

func newPostFormHandler(c echo.Context) error {
	if !isAuthenticated(c) {
		return c.Redirect(302, "/login")
	}

	tmpl := template.Must(template.ParseFiles("templates/new.html"))
	return tmpl.Execute(c.Response().Writer, nil)
}


func newPostHandler(c echo.Context) error {
    if !isAuthenticated(c) {
        return c.Redirect(302, "/login")
    }

    title := c.FormValue("title")

    // Parse blocks from form
    blocksJSON := c.FormValue("blocks")
    var blocks []Block
    if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "Invalid blocks format")
    }

    // Remove existing image blocks
    var filteredBlocks []Block
    for _, block := range blocks {
        if block.Type != "image" {
            filteredBlocks = append(filteredBlocks, block)
        }
    }

    // Handle file upload for images
    file, err := c.FormFile("image")
    if err != nil {
        // Log if no image was uploaded, but don't treat it as an error
        if err != http.ErrMissingFile {
            log.Printf("Error retrieving file: %v", err)
            return echo.NewHTTPError(http.StatusBadRequest, "Error processing file upload")
        }
        // If no file uploaded, use the original blocks
        blocks = filteredBlocks
    } else {
        // Image file was uploaded
        src, err := file.Open()
        if err != nil {
            log.Printf("ERROR: Failed to open uploaded file: %v", err)
            return echo.NewHTTPError(http.StatusInternalServerError, "Failed to open uploaded file")
        }
        defer src.Close()

        uploadDir := "uploads"
        if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
            log.Printf("ERROR: Failed to create upload directory: %v", err)
            return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create upload directory")
        }
    
        // Use a unique filename to prevent overwriting
        filename := time.Now().Format("20060102_150405_") + file.Filename
        filePath := filepath.Join(uploadDir, filename)
        log.Printf("Attempting to save uploaded file to: %s", filePath)
        
        dst, err := os.Create(filePath)
        if err != nil {
            log.Printf("ERROR: Failed to create destination file: %v", err)
            return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create destination file")
        }
        defer dst.Close()
    
        if _, err := io.Copy(dst, src); err != nil {
            log.Printf("ERROR: Failed to copy file contents: %v", err)
            return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save uploaded file")
        }
    
        log.Printf("Successfully saved file: %s", filename)
        imageURL := "/uploads/" + filename
        
        // Add the new image block to the filtered blocks
        filteredBlocks = append(filteredBlocks, Block{
            Type:    "image",
            Content: imageURL,
        })

        // Update blocks with filtered blocks (including new image)
        blocks = filteredBlocks
    }

    newPost := Post{
        ID:        len(posts) + 1,
        Title:     title,
        Blocks:    blocks,
        CreatedAt: time.Now(),
    }

    posts = append(posts, newPost)
    if err := savePosts(); err != nil {
        log.Printf("ERROR: Failed to save posts: %v", err)
        return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save posts")
    }

    return c.Redirect(302, "/home")
}

func deletePostHandler(c echo.Context) error {
	if !isAuthenticated(c) {
		return echo.NewHTTPError(401, "Unauthorized")
	}

	id, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return echo.NewHTTPError(400, "Invalid post ID")
	}

	for i, post := range posts {
		if post.ID == id {
			posts = append(posts[:i], posts[i+1:]...)
			savePosts()
			break
		}
	}
	return c.Redirect(302, "/admin")
}

func isAuthenticated(c echo.Context) bool {
	cookie, err := c.Cookie("isAdmin")
	return err == nil && cookie.Value == "true"
}
func aboutMeHandler(c echo.Context) error {
 tmpl := template.Must(template.ParseFiles("templates/about.html")) // Assuming the HTML file is named "about.html" and is in the project root
 return tmpl.Execute(c.Response().Writer, nil)
}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Create necessary directories
	os.MkdirAll("data", os.ModePerm)
	os.MkdirAll("uploads", os.ModePerm)
	os.MkdirAll("static", os.ModePerm)
	
	// Load posts from file
	if err := loadPosts(); err != nil {
		e.Logger.Warnf("Could not load posts: %v. Starting with an empty post list.", err)
		posts = []Post{}
	}

	// Routes
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/home")
	})
	e.GET("/home", mainHandler)
	e.GET("/post", postHandler)
	e.GET("/admin", adminHandler)
	e.GET("/login", loginHandler)
	e.POST("/login", loginHandler)
	e.GET("/logout", logoutHandler)
	e.GET("/new", newPostFormHandler)
	e.POST("/new", newPostHandler)
	e.GET("/about", aboutMeHandler)
	e.POST("/delete", deletePostHandler)

	// Static file routes
	e.Static("/static", "static")
	e.Static("/uploads", "uploads")

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port
	}
	e.Logger.Infof("Starting server on port %s", port)
	e.Logger.Fatal(e.Start(":" + port))
}
