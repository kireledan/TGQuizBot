package bot

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/anaskhan96/soup"
	_ "github.com/jackc/pgx/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/teris-io/shortid"
)

type Quiz struct {
	Questions []Question
}

type Question struct {
	Question string         `db:"questiontext"`
	Choices  pq.StringArray `db:"choices"`
	Correct  pq.Int64Array  `db:"answers"`
	ID       string         `db:"id"`
	Tags     pq.StringArray `db:"tags"`
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

var insertQuestion = `INSERT INTO Questions(questiontext, choices, answers, id, tags) 
		  VALUES($1, $2, $3, $4, $5)`

func (q Quiz) GetRandomQuestion() Question {
	s := rand.NewSource(time.Now().Unix())
	r := rand.New(s) // initialize local pseudorandom generator

	randomQ := q.Questions[r.Intn(len(q.Questions))]
	if len(randomQ.Choices) <= 1 {
		randomQ = q.Questions[r.Intn(len(q.Questions))]
	}
	return randomQ
}

func UploadQuizToDB(q Quiz) {
	conn, err := sqlx.Open("pgx", "Redacted")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	tx := conn.MustBegin()
	for _, question := range q.Questions {
		fmt.Println(question)
		if question.Question != "" {
			_, err := tx.Exec(insertQuestion, question.Question, pq.Array(question.Choices), pq.Array(question.Correct),
				question.ID, pq.Array(question.Tags))
			if err != nil {
				panic(err)
			}
		}
	}
	tx.Commit()
}

func GetQuestionByID(id string, db *sqlx.DB) Question {
	question := Question{}
	err := db.Get(&question, "select * FROM Questions WHERE id=$1 ", id)
	if err != nil {
		fmt.Println(id)
		panic(err)
	}
	return question
}

func BuildQuizFromDB(tag string, db *sqlx.DB) Quiz {
	questions := []Question{}
	err := db.Select(&questions, "select * FROM Questions WHERE $1=ANY(tags);", tag)
	if err != nil {
		panic(err)
	}
	return Quiz{Questions: questions}
}

func GetQuiz(quizFiles []string, tag string) Quiz {
	sid, _ := shortid.New(1, shortid.DefaultABC, 1512)

	// then either:
	AllQuestions := []Question{}

	for _, qfile := range quizFiles {
		dat, err := ioutil.ReadFile(qfile)
		check(err)

		doc := soup.HTMLParse(string(dat))
		Questions := doc.FindAll("div", "class", "panel-heading")
		FoundQuestions := make([]Question, len(Questions))

		for num, link := range Questions {
			titles := link.Find("div")
			if titles.Pointer != nil {
				FoundQuestions[num] = Question{Question: titles.Text()}
			}
		}

		Choices := doc.FindAll("ul", "class", "list-group")

		for num, answer := range Choices {
			choices := answer.FindAll("li", "class", "list-group-item")
			answers := []string{}
			correctAnswer := []int64{}

			for choiceNum, choice := range choices {
				answers = append(answers, choice.Text())
				for _, children := range choice.Children() {
					if children.Attrs()["title"] == "Correct answer" {
						correctAnswer = append(correctAnswer, int64(choiceNum))
					}
				}
			}
			FoundQuestions[num].Choices = answers
			FoundQuestions[num].Correct = correctAnswer
			id, _ := sid.Generate()
			FoundQuestions[num].ID = id
			FoundQuestions[num].Tags = []string{tag}
		}
		AllQuestions = append(AllQuestions, FoundQuestions...)
	}

	fmt.Println("Loaded questions")
	fmt.Println(len(AllQuestions))

	return Quiz{Questions: AllQuestions}
}

func remove(slice []Question, s int) []Question {
	return append(slice[:s], slice[s+1:]...)
}
